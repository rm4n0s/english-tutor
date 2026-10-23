// The small amount of browser code of English Tutor. Everything else is
// server-rendered and driven by Datastar (datastar.js, MIT licence).
(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const text = () => $('text');
  const under = () => $('underlay');

  const NAV_KEYS = new Set(['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Home', 'End', 'PageUp', 'PageDown']);
  let lastGoto = 0;

  // window.et is what the expressions in the page's attributes call.
  window.et = {
    // True when the caret moved because of a navigation key (not because of typing).
    isNav: (evt) => NAV_KEYS.has(evt.key),

    // Ask before work is thrown away.
    guard: (modified) => !modified || window.confirm('You have unsaved changes. Discard them?'),

    confirmDelete: (el) => {
      const li = el.closest('li');
      return window.confirm('Delete "' + ((li && li.dataset.title) || 'this item') + '"? This cannot be undone.');
    },

    pickFile: () => $('openfile').click(),

    // Show or hide what was typed in the API key field. The key that is stored
    // is never sent to the page, so there is only the new key to show.
    toggleKey: (btn) => {
      const input = $('api-key');
      const show = input.type === 'password';
      input.type = show ? 'text' : 'password';
      btn.textContent = show ? 'Hide' : 'Show';
      btn.setAttribute('aria-pressed', String(show));
    },

    focusEditor: () => text().focus(),

    // The tab title and the fonts follow the server's signals. fonts is the
    // declarations of the CSS variables of the fonts, made by the server.
    apply: (title, fonts) => {
      if (title) document.title = title;
      if (fonts) document.documentElement.style.cssText = fonts;
    },

    // Put the caret at the start of the selected issue and scroll it into view,
    // without taking the keyboard focus from where it is.
    goto: (g) => {
      if (!g || !g.seq || g.seq === lastGoto) return;
      lastGoto = g.seq;
      const ta = text();
      requestAnimationFrame(() => {
        ta.setSelectionRange(g.pos, g.pos);
        const mark = under().querySelector('.sel');
        if (mark) {
          const top = mark.offsetTop;
          if (top < ta.scrollTop || top > ta.scrollTop + ta.clientHeight - 2 * mark.offsetHeight) {
            ta.scrollTop = Math.max(0, top - ta.clientHeight / 3);
          }
        }
        under().scrollTop = ta.scrollTop;
      });
    },

    // Up and Down move through the list of issues.
    listKey: (evt) => {
      const rows = [...document.querySelectorAll('#issue-list .irow')];
      const i = rows.indexOf(document.activeElement);
      let next = -1;
      if (evt.key === 'ArrowDown') next = Math.min(rows.length - 1, i + 1);
      else if (evt.key === 'ArrowUp') next = Math.max(0, i - 1);
      else if (evt.key === 'Enter' && i >= 0) rows[i].click();
      if (next >= 0 && rows[next]) {
        evt.preventDefault();
        rows[next].focus();
        rows[next].click();
      }
    },
  };

  // The layer with the underlines scrolls with the editor.
  document.addEventListener('scroll', (evt) => {
    if (evt.target && evt.target.id === 'text') under().scrollTop = evt.target.scrollTop;
  }, true);

  // A tooltip for the underline under the mouse: the layer is under the editor
  // and cannot be hovered, so ask it what is at that point.
  let tipTimer = 0;
  document.addEventListener('mousemove', (evt) => {
    if (evt.target.id !== 'text') return;
    clearTimeout(tipTimer);
    tipTimer = setTimeout(() => {
      const ta = text();
      const layer = under();
      ta.style.pointerEvents = 'none';
      layer.style.pointerEvents = 'auto';
      const el = document.elementFromPoint(evt.clientX, evt.clientY);
      ta.style.pointerEvents = '';
      layer.style.pointerEvents = '';
      const tip = el && el.dataset && el.dataset.tip;
      if (tip) ta.title = tip; else ta.removeAttribute('title');
    }, 60);
  });

  // Keyboard shortcuts (section 4.1). Ctrl+N and Ctrl+O belong to the browser.
  document.addEventListener('keydown', (evt) => {
    const click = (id) => { const b = $(id); if (b && !b.disabled) b.click(); };
    if (evt.key === 'Escape') {
      const close = document.querySelector('#modal [data-close]');
      if (close) { evt.preventDefault(); close.click(); }
    } else if (evt.key === 'F7') {
      evt.preventDefault();
      click(evt.shiftKey ? 'btn-prev' : 'btn-next');
    } else if ((evt.ctrlKey || evt.metaKey) && evt.key === 'Enter') {
      evt.preventDefault();
      click('btn-check');
    } else if ((evt.ctrlKey || evt.metaKey) && evt.key.toLowerCase() === 's' && !evt.shiftKey) {
      evt.preventDefault();
      click('btn-save');
    }
  });

  // A link in an explanation comes from an LLM: show the whole address and ask.
  document.addEventListener('click', (evt) => {
    const a = evt.target.closest && evt.target.closest('#explain a');
    if (!a) return;
    evt.preventDefault();
    const href = a.getAttribute('href') || '';
    if (/^https?:\/\//i.test(href) && window.confirm('Open this address in a new tab?\n\n' + href)) {
      window.open(href, '_blank', 'noopener,noreferrer');
    }
  });

  // The server asks the browser to save a file by putting an address in #download.
  let lastDownload = '';
  const watchDownload = () => {
    const d = $('download');
    const href = d && d.getAttribute('data-href');
    if (!href || href === lastDownload) return;
    lastDownload = href;
    const a = document.createElement('a');
    a.href = href;
    a.download = d.getAttribute('data-name') || 'text.txt';
    document.body.appendChild(a);
    a.click();
    a.remove();
  };
  new MutationObserver(watchDownload).observe(document.documentElement, {
    subtree: true, childList: true, attributes: true, attributeFilter: ['data-href'],
  });

  // A dialog that appears puts the keyboard in its first field.
  new MutationObserver(() => {
    const field = document.querySelector('#modal [data-autofocus]');
    if (field && !field.dataset.focused) {
      field.dataset.focused = '1';
      field.focus();
      if (field.select) field.select();
    }
  }).observe(document.documentElement, { subtree: true, childList: true });

  // A page the server no longer knows (it was restarted): start again.
  document.addEventListener('datastar-fetch', (evt) => {
    const d = evt.detail;
    if (d && d.type === 'error' && d.argsRaw && d.argsRaw.status === '404') {
      window.location.assign('/');
    }
  });
})();
