/**
 * Miniform SDK
 * Auto-hooks to forms and provides single-submit pending states.
 * Include this script and it will automatically enhance Miniform forms.
 *
 * Usage:
 *   <script src="https://your-miniform.com/assets/miniform.js"></script>
 *
 * Forms are auto-detected by:
 *   - action URL matching /forms/{slug}/submit pattern
 *   - data-miniform attribute on the form element
 */
(function () {
  'use strict';

  if (document.__miniformSDKListening) return;
  document.__miniformSDKListening = true;

  var formDataSubmitterSupported;
  var turnstileWidgetCount = 0;

  function isMiniformForm(form) {
    if (form.hasAttribute('data-miniform')) return true;
    var action = form.getAttribute('action') || '';
    try {
      return /^\/forms\/[^/]+\/submit\/?$/.test(new URL(action, document.baseURI).pathname);
    } catch (_) {
      return false;
    }
  }

  function localRedirect(value) {
    if (!value) return '';
    try {
      var target = new URL(value, window.location.origin + '/');
      return target.origin === window.location.origin && /^https?:$/.test(target.protocol) ? target.href : '';
    } catch (_) {
      return '';
    }
  }

  function getSubmitButton(form) {
    return form.querySelector('[type="submit"], button:not([type="button"]):not([type="reset"])');
  }

  function isSubmitButton(control, form) {
    if (!control || control.form !== form || control.disabled) return false;
    var tagName = control.tagName.toLowerCase();
    var type = control.type.toLowerCase();
    return (tagName === 'button' && type === 'submit') ||
      (tagName === 'input' && (type === 'submit' || type === 'image'));
  }

  function getSubmitter(event, form) {
    if (isSubmitButton(event.submitter, form)) return event.submitter;
    return isSubmitButton(document.activeElement, form) ? document.activeElement : null;
  }

  function supportsFormDataSubmitter() {
    if (formDataSubmitterSupported !== undefined) return formDataSubmitterSupported;

    var form = document.createElement('form');
    var button = document.createElement('button');
    button.name = '__miniform_submitter_test__';
    button.value = 'included';
    form.appendChild(button);

    try {
      formDataSubmitterSupported = new FormData(form, button).get(button.name) === button.value;
    } catch (_) {
      formDataSubmitterSupported = false;
    }
    return formDataSubmitterSupported;
  }

  function createFormData(form, submitter) {
    if (!submitter) return new FormData(form);
    if (supportsFormDataSubmitter()) return new FormData(form, submitter);

    var formData = new FormData(form);
    if (!submitter.name) return formData;
    if (submitter.type === 'image') {
      formData.append(submitter.name + '.x', '0');
      formData.append(submitter.name + '.y', '0');
    } else {
      formData.append(submitter.name, submitter.value);
    }
    return formData;
  }

  function resetTurnstile(form) {
    if (!window.turnstile || typeof window.turnstile.reset !== 'function') return;

    var widgets = form.querySelectorAll('.cf-turnstile');
    for (var i = 0; i < widgets.length; i++) {
      var widget = widgets[i];
      var key = widget.getAttribute('data-miniform-turnstile');
      if (!key) {
        key = 'widget-' + (++turnstileWidgetCount);
        widget.setAttribute('data-miniform-turnstile', key);
      }
      try {
        window.turnstile.reset('[data-miniform-turnstile="' + key + '"]');
      } catch (_) {
        // A widget may not have finished rendering yet.
      }
    }
  }

  function showMessage(form, type, text) {
    var msg = form.querySelector('[data-miniform-msg]');
    var role = type === 'success' ? 'status' : 'alert';
    if (!msg) {
      msg = document.createElement('div');
      msg.setAttribute('data-miniform-msg', '');
      msg.setAttribute('role', role);
      form.appendChild(msg);
    } else {
      msg.setAttribute('role', role);
    }
    var base = 'mt-4 p-3 rounded-md text-sm';
    var styles = type === 'success'
      ? 'bg-green-50 text-green-800 border border-green-200'
      : 'bg-red-50 text-red-800 border border-red-200';
    msg.className = base + ' ' + styles;
    msg.textContent = text;
  }

  function clearMessage(form) {
    var msg = form.querySelector('[data-miniform-msg]');
    if (msg) {
      msg.parentNode.removeChild(msg);
    }
  }

  document.addEventListener('submit', function (event) {
    var form = event.target;
    if (!form || form.tagName !== 'FORM' || !isMiniformForm(form)) return;

    event.preventDefault();
    if (form.__miniformSDKInFlight) return;

    var submitter = getSubmitter(event, form);
    var submitBtn = submitter || getSubmitButton(form);
    var submitWasDisabled = submitBtn ? submitBtn.disabled : false;
    var formData = createFormData(form, submitter);
    var successUrl = localRedirect(formData.get('_success_url'));
    var errorUrl = localRedirect(formData.get('_error_url'));
    formData.delete('_success_url');
    formData.delete('_error_url');

    form.__miniformSDKInFlight = true;
    clearMessage(form);
    if (submitBtn) submitBtn.disabled = true;

    fetch(form.action, {
      method: 'POST',
      body: formData,
      headers: { 'Accept': 'application/json' },
      redirect: 'error'
    })
      .then(function (response) {
        return response.json().catch(function () {
          return {};
        }).then(function (data) {
          return { response: response, data: data || {} };
        });
      })
      .then(function (result) {
        var response = result.response;
        var data = result.data;

        if (response.ok && data.ok) {
          if (successUrl) {
            window.location.assign(successUrl);
          } else {
            form.reset();
            showMessage(form, 'success', 'Form submitted successfully!');
          }
        } else if (errorUrl) {
          window.location.assign(errorUrl);
        } else {
          showMessage(form, 'error', data.error || 'Submission failed. Please try again.');
        }
      })
      .catch(function () {
        showMessage(form, 'error', 'Submission status unknown. Check before trying again.');
      })
      .finally(function () {
        resetTurnstile(form);
        form.__miniformSDKInFlight = false;
        if (submitBtn) submitBtn.disabled = submitWasDisabled;
      });
  });
})();
