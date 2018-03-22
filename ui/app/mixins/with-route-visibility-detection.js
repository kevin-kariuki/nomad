import Mixin from '@ember/object/mixin';
import { assert } from '@ember/debug';

export default Mixin.create({
  visibilityHandler() {
    assert('visibilityHandler needs to be overridden in the Route', false);
  },

  setupDocumentVisibility: function() {
    this.set('_visibilityHandler', this.get('visibilityHandler').bind(this));
    document.addEventListener('visibilitychange', this.get('_visibilityHandler'));
  }.on('activate'),

  removeDocumentVisibility: function() {
    document.removeEventListener('visibilitychange', this.get('_visibilityHandler'));
  }.on('deactivate'),
});
