package taskrunner

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/client/allocdir"
	"github.com/hashicorp/nomad/client/allocrunner/interfaces"
	agentconsul "github.com/hashicorp/nomad/command/agent/consul"
	"github.com/hashicorp/nomad/nomad/structs"
	"github.com/pkg/errors"
)

const envoyBootstrapHookName = "envoy_bootstrap"

type envoyBootstrapHookConfig struct {
	alloc          *structs.Allocation
	consulHTTPAddr string
	logger         hclog.Logger
}

// envoyBootstrapHook writes the bootstrap config for the Connect Envoy proxy
// sidecar.
type envoyBootstrapHook struct {
	// alloc is the allocation with the envoy task being bootstrapped.
	alloc *structs.Allocation

	// Bootstrapping Envoy requires talking directly to Consul to generate
	// the bootstrap.json config. Runtime Envoy configuration is done via
	// Consul's gRPC endpoint.
	consulHTTPAddr string

	// logger is used to log things
	logger hclog.Logger
}

func newEnvoyBootstrapHook(c *envoyBootstrapHookConfig) *envoyBootstrapHook {
	return &envoyBootstrapHook{
		alloc:          c.alloc,
		consulHTTPAddr: c.consulHTTPAddr,
		logger:         c.logger.Named(envoyBootstrapHookName),
	}
}

func (envoyBootstrapHook) Name() string {
	return envoyBootstrapHookName
}

func (h *envoyBootstrapHook) Prestart(ctx context.Context, req *interfaces.TaskPrestartRequest, resp *interfaces.TaskPrestartResponse) error {
	if !req.Task.Kind.IsConnectProxy() {
		// Not a Connect proxy sidecar
		resp.Done = true
		return nil
	}

	serviceName := req.Task.Kind.Value()
	if serviceName == "" {
		return errors.New("connect proxy sidecar does not specify service name")
	}

	tg := h.alloc.Job.LookupTaskGroup(h.alloc.TaskGroup)

	var service *structs.Service
	for _, s := range tg.Services {
		if s.Name == serviceName {
			service = s
			break
		}
	}

	if service == nil {
		return errors.New("connect proxy sidecar task exists but no services configured with a sidecar")
	}

	h.logger.Debug("bootstrapping Connect proxy sidecar", "task", req.Task.Name, "service", serviceName)

	//TODO Should connect directly to Consul if the sidecar is running on the host netns.
	grpcAddr := "unix://" + allocdir.AllocGRPCSocket

	// Envoy bootstrap configuration may contain a Consul token, so write
	// it to the secrets directory like Vault tokens.
	bootstrapFilePath := filepath.Join(req.TaskDir.SecretsDir, "envoy_bootstrap.json")

	id := agentconsul.MakeAllocServiceID(h.alloc.ID, "group-"+tg.Name, service)

	h.logger.Debug("bootstrapping envoy", "sidecar_for", service.Name, "bootstrap_file", bootstrapFilePath, "sidecar_for_id", id, "grpc_addr", grpcAddr)

	siToken, err := h.maybeLoadSIToken(req.Task.Name, req.TaskDir.SecretsDir)
	if err != nil {
		h.logger.Error("failed to generate envoy bootstrap config", "sidecar_for", service.Name)
		return errors.Wrap(err, "failed to generate envoy bootstrap config")
	}
	h.logger.Debug("check for SI token for task", "task", req.Task.Name, "exists", siToken != "")

	bootstrapArgs := envoyBootstrapArgs{
		sidecarFor:     id,
		grpcAddr:       grpcAddr,
		consulHTTPAddr: h.consulHTTPAddr,
		siToken:        siToken,
	}.args()

	// put old stuff in here
	// Since Consul services are registered asynchronously with this task
	// hook running, retry a small number of times with backoff.
	for tries := 3; ; tries-- {
		cmd := exec.CommandContext(ctx, "consul", bootstrapArgs...)

		// Redirect output to secrets/envoy_bootstrap.json
		fd, err := os.Create(bootstrapFilePath)
		if err != nil {
			return fmt.Errorf("error creating secrets/envoy_bootstrap.json for envoy: %v", err)
		}
		cmd.Stdout = fd

		buf := bytes.NewBuffer(nil)
		cmd.Stderr = buf

		// Generate bootstrap
		err = cmd.Run()

		// Close bootstrap.json
		_ = fd.Close()

		if err == nil {
			// Happy path! Bootstrap was created, exit.
			break
		}

		// Check for error from command
		if tries == 0 {
			h.logger.Error("error creating bootstrap configuration for Connect proxy sidecar", "error", err, "stderr", buf.String())

			// Cleanup the bootstrap file. An errors here is not
			// important as (a) we test to ensure the deletion
			// occurs, and (b) the file will either be rewritten on
			// retry or eventually garbage collected if the task
			// fails.
			_ = os.Remove(bootstrapFilePath)

			// ExitErrors are recoverable since they indicate the
			// command was runnable but exited with a unsuccessful
			// error code.
			_, recoverable := err.(*exec.ExitError)
			return structs.NewRecoverableError(
				fmt.Errorf("error creating bootstrap configuration for Connect proxy sidecar: %v", err),
				recoverable,
			)
		}

		// Sleep before retrying to give Consul services time to register
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			// Killed before bootstrap, exit without setting Done
			return nil
		}
	}

	// Bootstrap written. Mark as done and move on.
	resp.Done = true
	return nil
}

func (h *envoyBootstrapHook) writeConfig(filename, config string) error {
	if err := ioutil.WriteFile(filename, []byte(config), 0440); err != nil {
		_ = os.Remove(filename)
		return err
	}
	return nil
}

func (_ *envoyBootstrapHook) retry(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(2 * time.Second):
		return true
	}
}

func (h *envoyBootstrapHook) execute(cmd *exec.Cmd) (string, error) {
	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		_, recoverable := err.(*exec.ExitError)
		// ExitErrors are recoverable since they indicate the
		// command was runnable but exited with a unsuccessful
		// error code.
		return stderr.String(), structs.NewRecoverableError(err, recoverable)
	}
	return stdout.String(), nil
}

type envoyBootstrapArgs struct {
	sidecarFor     string
	grpcAddr       string
	consulHTTPAddr string
	siToken        string
}

func (e envoyBootstrapArgs) args() []string {
	arguments := []string{
		"connect",
		"envoy",
		"-grpc-addr", e.grpcAddr,
		"-http-addr", e.consulHTTPAddr,
		"-bootstrap",
		"-sidecar-for", e.sidecarFor,
	}
	if e.siToken != "" {
		arguments = append(arguments, []string{"-token", e.siToken}...)
	}
	return arguments
}

// maybeLoadSIToken reads the SI token saved to disk in the secretes directory
// by the service identities prestart hook. This envoy bootstrap hook blocks
// until the sids hook completes, so if the SI token is required to exist (i.e.
// Consul ACLs are enabled), it will be in place by the time we try to read it.
func (h *envoyBootstrapHook) maybeLoadSIToken(task, dir string) (string, error) {
	tokenPath := filepath.Join(dir, sidsTokenFile)
	token, err := ioutil.ReadFile(tokenPath)
	if err != nil {
		if !os.IsNotExist(err) {
			h.logger.Error("failed to load SI token", "task", task, "error", err)
			return "", errors.Wrapf(err, "failed to load SI token for %s", task)
		}
		h.logger.Trace("no SI token to load", "task", task)
		return "", nil // token file does not exist
	}
	h.logger.Trace("recovered pre-existing SI token", "task", task)
	return string(token), nil
}
