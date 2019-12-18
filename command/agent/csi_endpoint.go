package agent

import (
	"net/http"
	"strings"

	"github.com/hashicorp/nomad/nomad/structs"
)

func (s *HTTPServer) CSIVolumesRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "GET" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	args := structs.CSIVolumeListRequest{}

	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	var out structs.CSIVolumeListResponse
	if err := s.agent.RPC("CSIVolume.List", &args, &out); err != nil {
		return nil, err
	}

	setMeta(resp, &out.QueryMeta)
	return out.Volumes, nil
}

// CSIVolumeSpecificRequest dispatches GET and PUT
func (s *HTTPServer) CSIVolumeSpecificRequest(resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	reqSuffix := strings.TrimPrefix(req.URL.Path, "/v1/csi/volume/")

	// tokenize the suffix of the path to get the alloc id and find the action
	// invoked on the alloc id
	tokens := strings.Split(reqSuffix, "/")
	if len(tokens) > 2 || len(tokens) < 1 {
		return nil, CodedError(404, resourceNotFoundErr)
	}
	id := tokens[0]

	switch req.Method {
	case "GET":
		return s.csiVolumeGet(id, resp, req)
	case "PUT":
		return s.csiVolumePut(id, resp, req)
	case "DELETE":
		return s.csiVolumeDel(id, resp, req)
	default:
		return nil, CodedError(405, ErrInvalidMethod)
	}
}

func (s *HTTPServer) csiVolumeGet(id string, resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	args := structs.CSIVolumeGetRequest{
		ID: id,
	}
	if s.parse(resp, req, &args.Region, &args.QueryOptions) {
		return nil, nil
	}

	var out structs.CSIVolumeGetResponse
	if err := s.agent.RPC("CSIVolume.Get", &args, &out); err != nil {
		return nil, err
	}

	setMeta(resp, &out.QueryMeta)
	if out.Volume == nil {
		return nil, CodedError(404, "alloc not found")
	}

	return out.Volume, nil
}

func (s *HTTPServer) csiVolumePut(id string, resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "PUT" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	args0 := structs.CSIVolumeRegisterRequest{}
	if err := decodeBody(req, &args0); err != nil {
		return err, CodedError(400, err.Error())
	}

	args := structs.CSIVolumeRegisterRequest{
		Volumes: args0.Volumes,
	}
	s.parseWriteRequest(req, &args.WriteRequest)

	var out structs.CSIVolumeRegisterResponse
	if err := s.agent.RPC("CSIVolume.Register", &args, &out); err != nil {
		return nil, err
	}

	setMeta(resp, &out.QueryMeta)

	return nil, nil
}

func (s *HTTPServer) csiVolumeDel(id string, resp http.ResponseWriter, req *http.Request) (interface{}, error) {
	if req.Method != "DELETE" {
		return nil, CodedError(405, ErrInvalidMethod)
	}

	args := structs.CSIVolumeDeregisterRequest{
		VolumeIDs: []string{id},
	}
	s.parseWriteRequest(req, &args.WriteRequest)

	var out structs.CSIVolumeDeregisterResponse
	if err := s.agent.RPC("CSIVolume.Deregister", &args, &out); err != nil {
		return nil, err
	}

	setMeta(resp, &out.QueryMeta)

	return nil, nil
}
