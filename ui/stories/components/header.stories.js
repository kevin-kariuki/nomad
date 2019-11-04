/* eslint-env node */
import hbs from 'htmlbars-inline-precompile';

export default {
  title: 'Components|Header',
};

export const Header = () => {
  return {
    template: hbs`
      <h5 class="title is-5">Global header</h5>
      <nav class="navbar is-primary">
        <div class="navbar-brand">
          <span class="gutter-toggle" aria-label="menu">
            {{partial "partials/hamburger-menu"}}
          </span>
          <span class="navbar-item is-logo">
            {{partial "partials/nomad-logo"}}
          </span>
        </div>
        <div class="navbar-end">
          <a class="navbar-item">Secondary</a>
          <a class="navbar-item">Links</a>
          <a class="navbar-item">Here</a>
        </div>
      </nav>
      `,
  };
};
