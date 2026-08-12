// Shared "build your own badge" widget for the landing page and the setup
// page. Two things live in this file:
//
// 1. A client-side port of cmd/crowdin-stats/render.go's SVG layout, used
//    only for the live preview so tweaking colors/limit/unit updates
//    instantly with no network round trip (and, on the landing page,
//    without a registered project to fetch real data from). Keep this in
//    sync with render.go if that file's layout constants change.
// 2. Widget wiring: reads the controls inside a `.badge-builder` root,
//    re-renders the preview and the generated URL/markdown on every
//    change.
(function () {
  'use strict';

  var DEFAULT_COLORS = { bg: '#12161f', text: '#e8eaed', muted: '#8b93a3', accent: '#7dd3a8', border: '#232834' };

  var DEMO_LANGUAGES = [
    { name: 'French', percent: 96 },
    { name: 'German', percent: 88 },
    { name: 'Spanish', percent: 82 },
    { name: 'Japanese', percent: 67 },
    { name: 'Portuguese', percent: 54 },
    { name: 'Korean', percent: 31 },
  ];

  var DEMO_CONTRIBUTORS = [
    { username: 'amara', fullName: 'Amara Okafor', amount: 4210, avatar: 'https://i.pravatar.cc/150?img=47' },
    { username: 'kenji', fullName: 'Kenji Watanabe', amount: 3870, avatar: 'https://i.pravatar.cc/150?img=52' },
    { username: 'lucia', fullName: 'Lucia Fernandez', amount: 3120, avatar: 'https://i.pravatar.cc/150?img=45' },
    { username: 'piotr', fullName: 'Piotr Nowak', amount: 2650, avatar: 'https://i.pravatar.cc/150?img=13' },
    { username: 'hana', fullName: 'Hana Kim', amount: 2400, avatar: 'https://i.pravatar.cc/150?img=44' },
    { username: 'diego', fullName: 'Diego Alvarez', amount: 1980, avatar: 'https://i.pravatar.cc/150?img=14' },
    { username: 'elin', fullName: 'Elin Svensson', amount: 1600, avatar: 'https://i.pravatar.cc/150?img=48' },
    { username: 'raj', fullName: 'Raj Patel', amount: 1340, avatar: 'https://i.pravatar.cc/150?img=51' },
    { username: 'noor', fullName: 'Noor Hassan', amount: 1120, avatar: '' },
    { username: 'tomas', fullName: 'Tomas Novak', amount: 940, avatar: 'https://i.pravatar.cc/150?img=53' },
    { username: 'yui', fullName: 'Yui Tanaka', amount: 810, avatar: 'https://i.pravatar.cc/150?img=43' },
    { username: 'ines', fullName: 'Ines Costa', amount: 700, avatar: '' },
  ];

  function esc(s) {
    var d = document.createElement('div');
    d.textContent = s == null ? '' : String(s);
    return d.innerHTML;
  }

  function clampPercent(p) {
    return Math.max(0, Math.min(100, p));
  }

  function truncateLabel(s, max) {
    var chars = Array.from(s);
    if (chars.length <= max) return esc(s);
    return esc(chars.slice(0, max - 1).join('') + '…');
  }

  // --- table.svg, ported from renderTableSVG in render.go ---
  var TABLE = {
    rowHeight: 28, width: 360, labelWidth: 110, barWidth: 160,
    barGap: 8, paddingX: 12, paddingTop: 12,
  };

  function renderTableSVG(languages, colors) {
    var sorted = languages.slice().sort(function (a, b) {
      if (a.percent !== b.percent) return b.percent - a.percent;
      return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
    });

    if (sorted.length === 0) {
      return emptyStateSVG(TABLE.width, 60, 'no language data yet', colors);
    }

    var height = TABLE.paddingTop * 2 + TABLE.rowHeight * sorted.length;
    var barX = TABLE.paddingX + TABLE.labelWidth + TABLE.barGap;
    var percentX = TABLE.width - TABLE.paddingX;

    var out = '<svg xmlns="http://www.w3.org/2000/svg" width="' + TABLE.width + '" height="' + height +
      '" viewBox="0 0 ' + TABLE.width + ' ' + height + '" font-family="\'Segoe UI\', Helvetica, Arial, sans-serif">';
    out += '<rect width="' + TABLE.width + '" height="' + height + '" fill="' + colors.bg + '" rx="8"/>';

    sorted.forEach(function (lang, i) {
      var y = TABLE.paddingTop + i * TABLE.rowHeight;
      var midY = y + TABLE.rowHeight / 2;
      out += '<text x="' + TABLE.paddingX + '" y="' + midY + '" fill="' + colors.text + '" font-size="12" dominant-baseline="middle">' + truncateLabel(lang.name, 16) + '</text>';
      out += '<rect x="' + barX + '" y="' + (midY - 5) + '" width="' + TABLE.barWidth + '" height="10" rx="5" fill="' + colors.border + '"/>';
      var filled = Math.floor(TABLE.barWidth * clampPercent(lang.percent) / 100);
      out += '<rect x="' + barX + '" y="' + (midY - 5) + '" width="' + filled + '" height="10" rx="5" fill="' + colors.accent + '"/>';
      out += '<text x="' + percentX + '" y="' + midY + '" fill="' + colors.muted + '" font-size="11" text-anchor="end" dominant-baseline="middle">' + clampPercent(lang.percent) + '%</text>';
    });

    out += '</svg>';
    return out;
  }

  // --- contributors.svg, ported from renderContributorsSVG in render.go ---
  var GRID = { avatarSize: 48, avatarGap: 6, paddingX: 10, paddingY: 10, cols: 8 };

  function renderContributorsSVG(contributors, limit, colors) {
    var sorted = contributors.slice().sort(function (a, b) {
      if (a.amount !== b.amount) return b.amount - a.amount;
      return a.username < b.username ? -1 : a.username > b.username ? 1 : 0;
    });
    if (limit > 0 && sorted.length > limit) sorted = sorted.slice(0, limit);

    if (sorted.length === 0) {
      return emptyStateSVG(320, 60, 'no contributors yet', colors);
    }

    var cols = Math.min(GRID.cols, sorted.length);
    var rows = Math.ceil(sorted.length / GRID.cols);
    var cell = GRID.avatarSize + GRID.avatarGap;
    var width = GRID.paddingX * 2 + cell * cols - GRID.avatarGap;
    var height = GRID.paddingY * 2 + cell * rows - GRID.avatarGap;

    var out = '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height + '" viewBox="0 0 ' + width + ' ' + height + '">';
    out += '<rect width="' + width + '" height="' + height + '" fill="' + colors.bg + '" rx="8"/>';

    sorted.forEach(function (c, i) {
      var col = i % GRID.cols;
      var row = Math.floor(i / GRID.cols);
      var cx = GRID.paddingX + col * cell + GRID.avatarSize / 2;
      var cy = GRID.paddingY + row * cell + GRID.avatarSize / 2;
      var clipID = 'clip' + i;
      var title = c.fullName || c.username;

      out += '<clipPath id="' + clipID + '"><circle cx="' + cx + '" cy="' + cy + '" r="' + (GRID.avatarSize / 2) + '"/></clipPath>';
      out += '<a href="https://crowdin.com/profile/' + esc(c.username) + '" target="_blank">';
      out += '<title>' + esc(title) + '</title>';
      if (c.avatar) {
        out += '<image href="' + esc(c.avatar) + '" x="' + (GRID.paddingX + col * cell) + '" y="' + (GRID.paddingY + row * cell) +
          '" width="' + GRID.avatarSize + '" height="' + GRID.avatarSize + '" clip-path="url(#' + clipID + ')" preserveAspectRatio="xMidYMid slice"/>';
      } else {
        out += '<circle cx="' + cx + '" cy="' + cy + '" r="' + (GRID.avatarSize / 2) + '" fill="' + colors.border + '"/>';
        var initial = title ? title.charAt(0).toUpperCase() : '?';
        out += '<text x="' + cx + '" y="' + cy + '" fill="' + colors.muted + '" font-size="16" text-anchor="middle" dominant-baseline="central">' + esc(initial) + '</text>';
      }
      out += '<circle cx="' + cx + '" cy="' + cy + '" r="' + (GRID.avatarSize / 2) + '" fill="none" stroke="' + colors.border + '" stroke-width="1"/>';
      out += '</a>';
    });

    out += '</svg>';
    return out;
  }

  function emptyStateSVG(width, height, message, colors) {
    return '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height + '" viewBox="0 0 ' + width + ' ' + height +
      '" font-family="\'Segoe UI\', Helvetica, Arial, sans-serif">' +
      '<rect width="' + width + '" height="' + height + '" fill="' + colors.bg + '" rx="8"/>' +
      '<text x="' + (width / 2) + '" y="' + (height / 2) + '" fill="' + colors.muted + '" font-size="12" text-anchor="middle" dominant-baseline="middle">' + esc(message) + '</text>' +
      '</svg>';
  }

  // --- widget wiring ---

  function buildQueryString(type, state) {
    var params = new URLSearchParams();
    if (type === 'contributors') {
      if (state.limit !== 30) params.set('limit', state.limit);
      if (state.unit !== 'words') params.set('unit', state.unit);
      if (state.hideOwner) params.set('hideOwner', 'true');
    }
    ['bg', 'text', 'muted', 'accent', 'border'].forEach(function (key) {
      var hex = state.colors[key].replace('#', '');
      if (hex.toLowerCase() !== DEFAULT_COLORS[key].replace('#', '')) {
        params.set(key, hex);
      }
    });
    var qs = params.toString();
    return qs ? '?' + qs : '';
  }

  function init(root, opts) {
    opts = opts || {};
    var mode = opts.mode === 'live' ? 'live' : 'demo';
    var baseEmbedURL = opts.baseEmbedURL || '/embed/{public_id}';

    var state = {
      type: 'table',
      limit: 30,
      unit: 'words',
      hideOwner: false,
      colors: Object.assign({}, DEFAULT_COLORS),
    };

    var typeButtons = root.querySelectorAll('[data-badge-type]');
    var contribControls = root.querySelector('[data-builder-contrib-controls]');
    var limitInput = root.querySelector('[data-builder-limit]');
    var limitValue = root.querySelector('[data-builder-limit-value]');
    var unitSelect = root.querySelector('[data-builder-unit]');
    var hideOwnerInput = root.querySelector('[data-builder-hideowner]');
    var colorInputs = root.querySelectorAll('[data-builder-color]');
    var previewEl = root.querySelector('[data-builder-preview]');
    var previewImg = root.querySelector('[data-builder-preview-img]');
    var urlEl = root.querySelector('[data-builder-url]');
    var copyBtn = root.querySelector('[data-builder-copy]');
    var debounceTimer = null;

    function render() {
      typeButtons.forEach(function (btn) {
        var active = btn.getAttribute('data-badge-type') === state.type;
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
        btn.classList.toggle('bg-accent', active);
        btn.classList.toggle('text-accent-contrast', active);
        btn.classList.toggle('text-text-muted', !active);
      });
      if (contribControls) {
        contribControls.classList.toggle('hidden', state.type !== 'contributors');
      }

      var qs = buildQueryString(state.type, state);
      var filename = state.type === 'table' ? 'table.svg' : 'contributors.svg';
      var fullURL = baseEmbedURL + '/' + filename + qs;
      if (urlEl) urlEl.textContent = fullURL;

      if (mode === 'demo' && previewEl) {
        var svg = state.type === 'table'
          ? renderTableSVG(DEMO_LANGUAGES, state.colors)
          : renderContributorsSVG(DEMO_CONTRIBUTORS, state.limit, state.colors);
        previewEl.innerHTML = svg;
      } else if (mode === 'live' && previewImg) {
        clearTimeout(debounceTimer);
        debounceTimer = setTimeout(function () {
          previewImg.src = fullURL + (fullURL.indexOf('?') === -1 ? '?' : '&') + 't=' + Date.now();
        }, 400);
      }
    }

    typeButtons.forEach(function (btn) {
      btn.addEventListener('click', function () {
        state.type = btn.getAttribute('data-badge-type');
        render();
      });
    });

    if (limitInput) {
      limitInput.addEventListener('input', function () {
        state.limit = parseInt(limitInput.value, 10) || 30;
        if (limitValue) limitValue.textContent = state.limit;
        render();
      });
    }
    if (unitSelect) {
      unitSelect.addEventListener('change', function () {
        state.unit = unitSelect.value;
        render();
      });
    }
    if (hideOwnerInput) {
      hideOwnerInput.addEventListener('change', function () {
        state.hideOwner = hideOwnerInput.checked;
        render();
      });
    }
    colorInputs.forEach(function (input) {
      var key = input.getAttribute('data-builder-color');
      input.value = state.colors[key];
      input.addEventListener('input', function () {
        state.colors[key] = input.value;
        render();
      });
    });

    if (copyBtn) {
      copyBtn.addEventListener('click', function () {
        var text = urlEl ? urlEl.textContent : '';
        navigator.clipboard.writeText(text);
        var original = copyBtn.textContent;
        copyBtn.textContent = 'Copied!';
        setTimeout(function () { copyBtn.textContent = original; }, 1500);
      });
    }

    render();
  }

  window.CrowdinStatsBadgeBuilder = { init: init };
})();
