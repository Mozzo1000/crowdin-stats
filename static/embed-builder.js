// Shared "build your own embed" widget for the landing page and the setup
// page. Two things live in this file:
//
// 1. A client-side port of cmd/crowdin-stats/render.go's SVG layout. On the
//    landing page it renders static demo fixtures (no registered project to
//    fetch real data from). On the setup page it renders real project data
//    fetched once from /embed/{publicID}/data.json — tweaking colors,
//    limit, progress, etc. re-renders locally with no further network
//    round trip, so it can never burn the project's Crowdin refresh-token
//    budget (see ensureLiveData below). Keep this in sync with render.go if
//    that file's layout constants change.
// 2. Widget wiring: reads the controls inside a `.embed-builder` root,
//    re-renders the preview and the generated URL/markdown on every
//    change.
(function () {
  'use strict';

  // Mirrors render.go's defaultEmbedColors / darkEmbedColors — the site's
  // own light- and dark-mode CSS tokens (input.css :root / :root.dark).
  var DEFAULT_COLORS = { bg: '#ffffff', text: '#1f2a33', muted: '#64748b', accent: '#2f6fed', border: '#8a90a0' };
  var DARK_COLORS = { bg: '#12161d', text: '#edeff3', muted: '#97a2b4', accent: '#5b8dff', border: '#59657e' };

  // Ported from render.go's mixHex/hexChannels — blends two hex colors,
  // weighting b by t (0..1). Used to derive the fallback-avatar fill from
  // bg/text rather than reusing colors.border, which is now tuned for
  // stroke contrast and reads as too solid/prominent as a fill.
  function hexChannels(hex) {
    hex = hex.replace('#', '');
    if (hex.length === 3) {
      hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
    }
    return [parseInt(hex.slice(0, 2), 16), parseInt(hex.slice(2, 4), 16), parseInt(hex.slice(4, 6), 16)];
  }

  function mixHex(a, b, t) {
    var ac = hexChannels(a), bc = hexChannels(b);
    var mix = function (x, y) { return Math.round(x * (1 - t) + y * t); };
    return '#' + [mix(ac[0], bc[0]), mix(ac[1], bc[1]), mix(ac[2], bc[2])].map(function (v) {
      return v.toString(16).padStart(2, '0');
    }).join('');
  }

  // WCAG 2.x relative luminance / contrast ratio, used to warn when a
  // foreground/background pair the user picked would be hard to read.
  function srgbToLinear(c) {
    c = c / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  }

  function relativeLuminance(hex) {
    var ch = hexChannels(hex);
    return 0.2126 * srgbToLinear(ch[0]) + 0.7152 * srgbToLinear(ch[1]) + 0.0722 * srgbToLinear(ch[2]);
  }

  function contrastRatio(hexA, hexB) {
    var lA = relativeLuminance(hexA) + 0.05;
    var lB = relativeLuminance(hexB) + 0.05;
    return lA > lB ? lA / lB : lB / lA;
  }

  var HEX_COLOR_RE = /^#?[0-9a-fA-F]{6}$/;

  function normalizeHex(raw) {
    if (!HEX_COLOR_RE.test(raw)) return null;
    return '#' + raw.replace('#', '').toLowerCase();
  }

  var DEMO_LANGUAGES = [
    { id: 'fr', name: 'French', percent: 96, approvalPercent: 80, wordsTotal: 850, wordsTranslated: 816, wordsApproved: 680, phrasesTotal: 620, phrasesTranslated: 595, phrasesApproved: 496 },
    { id: 'de', name: 'German', percent: 88, approvalPercent: 65, wordsTotal: 850, wordsTranslated: 748, wordsApproved: 552, phrasesTotal: 620, phrasesTranslated: 546, phrasesApproved: 403 },
    { id: 'es', name: 'Spanish', percent: 82, approvalPercent: 60, wordsTotal: 850, wordsTranslated: 697, wordsApproved: 510, phrasesTotal: 620, phrasesTranslated: 508, phrasesApproved: 372 },
    { id: 'ja', name: 'Japanese', percent: 67, approvalPercent: 40, wordsTotal: 850, wordsTranslated: 570, wordsApproved: 340, phrasesTotal: 620, phrasesTranslated: 415, phrasesApproved: 248 },
    { id: 'pt', name: 'Portuguese', percent: 54, approvalPercent: 30, wordsTotal: 850, wordsTranslated: 459, wordsApproved: 255, phrasesTotal: 620, phrasesTranslated: 335, phrasesApproved: 186 },
    { id: 'ko', name: 'Korean', percent: 31, approvalPercent: 12, wordsTotal: 850, wordsTranslated: 264, wordsApproved: 102, phrasesTotal: 620, phrasesTranslated: 192, phrasesApproved: 74 },
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

  // --- table.svg, ported from renderTableSVG/prepareTableLanguages in render.go ---
  var TABLE = {
    rowHeight: 28, width: 360, labelWidth: 110, barWidth: 160,
    barGap: 8, paddingX: 12, paddingTop: 12,
  };

  // parseLanguagePins, ported from render.go — comma-separated codes/names
  // into a lowercased lookup set, capped at 50 entries.
  function parseLanguagePins(raw) {
    if (!raw) return {};
    var out = {};
    var count = 0;
    raw.split(',').some(function (p) {
      p = p.trim().toLowerCase();
      if (p) {
        out[p] = true;
        count++;
      }
      return count >= 50;
    });
    return out;
  }

  // prepareTableLanguages, ported from render.go — swaps in approval %
  // when requested, then either restricts to the pinned languages
  // exclusively, or filters/sorts/truncates by minPercent/limit.
  function prepareTableLanguages(languages, progress, minPercent, limit, pinned) {
    var prepared = languages.map(function (lang) {
      var copy = Object.assign({}, lang);
      if (progress === 'approval') copy.percent = copy.approvalPercent;
      return copy;
    });

    var hasPins = Object.keys(pinned).length > 0;
    if (hasPins) {
      return prepared.filter(function (lang) {
        var id = (lang.id || '').toLowerCase();
        var name = (lang.name || '').toLowerCase();
        return pinned[id] || pinned[name];
      });
    }

    var rest = prepared.filter(function (lang) {
      return lang.percent >= minPercent;
    });
    rest.sort(function (a, b) {
      if (a.percent !== b.percent) return b.percent - a.percent;
      return a.name < b.name ? -1 : a.name > b.name ? 1 : 0;
    });
    if (limit > 0 && rest.length > limit) rest = rest.slice(0, limit);

    return rest;
  }

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
    out += '<rect x="0.5" y="0.5" width="' + (TABLE.width - 1) + '" height="' + (height - 1) + '" rx="8" fill="' + colors.bg + '" stroke="' + colors.border + '"/>';

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
    out += '<rect x="0.5" y="0.5" width="' + (width - 1) + '" height="' + (height - 1) + '" rx="8" fill="' + colors.bg + '" stroke="' + colors.border + '"/>';

    var fallbackFill = mixHex(colors.bg, colors.text, 0.12);

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
        out += '<circle cx="' + cx + '" cy="' + cy + '" r="' + (GRID.avatarSize / 2) + '" fill="' + fallbackFill + '"/>';
        var initial = title ? title.charAt(0).toUpperCase() : '?';
        out += '<text x="' + cx + '" y="' + cy + '" fill="' + colors.text + '" font-size="16" text-anchor="middle" dominant-baseline="central">' + esc(initial) + '</text>';
      }
      out += '<circle cx="' + cx + '" cy="' + cy + '" r="' + (GRID.avatarSize / 2) + '" fill="none" stroke="' + colors.border + '" stroke-width="1"/>';
      out += '</a>';
    });

    out += '</svg>';
    return out;
  }

  // --- overall.svg, ported from renderOverallCardSVG/renderOverallCircleSVG in render.go ---
  var CARD = { width: 340, height: 140, padding: 20 };
  var CIRCLE = { size: 120, radius: 46 };

  function aggregateOverall(languages, unit, progress) {
    var total = 0, translated = 0;
    var approval = progress === 'approval';
    languages.forEach(function (lang) {
      if (unit === 'strings') {
        total += lang.phrasesTotal;
        translated += approval ? lang.phrasesApproved : lang.phrasesTranslated;
      } else {
        total += lang.wordsTotal;
        translated += approval ? lang.wordsApproved : lang.wordsTranslated;
      }
    });
    var percent = total > 0 ? clampPercent(Math.floor(translated * 100 / total)) : 0;
    return { total: total, translated: translated, percent: percent };
  }

  function formatThousands(n) {
    var s = String(n);
    var out = '';
    for (var i = 0; i < s.length; i++) {
      if (i > 0 && (s.length - i) % 3 === 0) out += ',';
      out += s[i];
    }
    return out;
  }

  function renderOverallCardSVG(languages, unit, metric, progress, colors) {
    var prog = aggregateOverall(languages, unit, progress);
    if (prog.total === 0) {
      return emptyStateSVG(CARD.width, 60, 'no translation data yet', colors);
    }

    var label = progress === 'approval' ? 'APPROVAL PROGRESS' : 'TRANSLATION PROGRESS';
    var out = '<svg xmlns="http://www.w3.org/2000/svg" width="' + CARD.width + '" height="' + CARD.height +
      '" viewBox="0 0 ' + CARD.width + ' ' + CARD.height + '" font-family="\'Segoe UI\', Helvetica, Arial, sans-serif">';
    out += '<rect x="0.5" y="0.5" width="' + (CARD.width - 1) + '" height="' + (CARD.height - 1) + '" rx="10" fill="' + colors.bg + '" stroke="' + colors.border + '"/>';
    out += '<text x="' + CARD.padding + '" y="30" fill="' + colors.muted + '" font-size="12" font-weight="600" letter-spacing="0.06em">' + label + '</text>';

    if (metric === 'fraction') {
      out += '<text x="' + CARD.padding + '" y="82" fill="' + colors.accent + '" font-size="34" font-weight="700">' +
        formatThousands(prog.translated) + ' / ' + formatThousands(prog.total) + ' ' + esc(unit) + '</text>';
    } else {
      out += '<text x="' + CARD.padding + '" y="82" fill="' + colors.accent + '" font-size="44" font-weight="700">' + prog.percent + '%</text>';
      if (metric === 'both') {
        out += '<text x="' + CARD.padding + '" y="106" fill="' + colors.text + '" font-size="13">' +
          formatThousands(prog.translated) + ' / ' + formatThousands(prog.total) + ' ' + esc(unit) + '</text>';
      }
    }

    var barY = CARD.height - 22;
    var barWidth = CARD.width - CARD.padding * 2;
    out += '<rect x="' + CARD.padding + '" y="' + barY + '" width="' + barWidth + '" height="8" rx="4" fill="' + colors.border + '"/>';
    var filled = Math.floor(barWidth * prog.percent / 100);
    out += '<rect x="' + CARD.padding + '" y="' + barY + '" width="' + filled + '" height="8" rx="4" fill="' + colors.accent + '"/>';

    out += '</svg>';
    return out;
  }

  function renderOverallCircleSVG(languages, unit, progress, colors) {
    var prog = aggregateOverall(languages, unit, progress);
    if (prog.total === 0) {
      return emptyStateSVG(CIRCLE.size, CIRCLE.size, 'no data', colors);
    }

    var circumference = 2 * Math.PI * CIRCLE.radius;
    var filled = circumference * prog.percent / 100;
    var center = CIRCLE.size / 2;

    var out = '<svg xmlns="http://www.w3.org/2000/svg" width="' + CIRCLE.size + '" height="' + CIRCLE.size +
      '" viewBox="0 0 ' + CIRCLE.size + ' ' + CIRCLE.size + '" font-family="\'Segoe UI\', Helvetica, Arial, sans-serif">';
    out += '<rect x="0" y="0" width="' + CIRCLE.size + '" height="' + CIRCLE.size + '" rx="10" fill="' + colors.bg + '"/>';
    out += '<circle cx="' + center + '" cy="' + center + '" r="' + CIRCLE.radius + '" fill="none" stroke="' + colors.border + '" stroke-width="10"/>';
    out += '<circle cx="' + center + '" cy="' + center + '" r="' + CIRCLE.radius + '" fill="none" stroke="' + colors.accent +
      '" stroke-width="10" stroke-linecap="round" stroke-dasharray="' + filled.toFixed(2) + ' ' + circumference.toFixed(2) +
      '" transform="rotate(-90 ' + center + ' ' + center + ')"/>';
    out += '<text x="' + center + '" y="' + (center + 2) + '" fill="' + colors.text +
      '" font-size="26" font-weight="700" text-anchor="middle" dominant-baseline="middle">' + prog.percent + '%</text>';
    out += '</svg>';
    return out;
  }

  function emptyStateSVG(width, height, message, colors) {
    return '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height + '" viewBox="0 0 ' + width + ' ' + height +
      '" font-family="\'Segoe UI\', Helvetica, Arial, sans-serif">' +
      '<rect x="0.5" y="0.5" width="' + (width - 1) + '" height="' + (height - 1) + '" rx="8" fill="' + colors.bg + '" stroke="' + colors.border + '"/>' +
      '<text x="' + (width / 2) + '" y="' + (height / 2) + '" fill="' + colors.muted + '" font-size="12" text-anchor="middle" dominant-baseline="middle">' + esc(message) + '</text>' +
      '</svg>';
  }

  // --- widget wiring ---

  function buildQueryString(type, state) {
    var params = new URLSearchParams();
    if (type === 'table') {
      if (state.tableProgress !== 'translation') params.set('progress', state.tableProgress);
      if (state.tableLimit > 0) params.set('limit', state.tableLimit);
      if (state.tableMinPercent > 0) params.set('minPercent', state.tableMinPercent);
      if (state.tableLanguages.trim()) params.set('languages', state.tableLanguages.trim());
    }
    if (type === 'contributors') {
      if (state.limit !== 30) params.set('limit', state.limit);
      if (state.unit !== 'words') params.set('unit', state.unit);
      if (state.hideOwner) params.set('hideOwner', 'true');
    }
    if (type === 'overall') {
      if (state.overallUnit !== 'words') params.set('unit', state.overallUnit);
      if (state.overallProgress !== 'translation') params.set('progress', state.overallProgress);
      if (state.overallVariant !== 'card') params.set('variant', state.overallVariant);
      if (state.overallVariant !== 'circle' && state.overallMetric !== 'both') params.set('metric', state.overallMetric);
    }
    if (state.theme === 'dark') params.set('theme', 'dark');
    var base = state.theme === 'dark' ? DARK_COLORS : DEFAULT_COLORS;
    ['bg', 'text', 'muted', 'accent', 'border'].forEach(function (key) {
      var hex = state.colors[key].replace('#', '');
      if (hex.toLowerCase() !== base[key].replace('#', '')) {
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

    // Follow the site's own light/dark toggle until the user explicitly
    // overrides the builder's theme/colors — after that their choice wins.
    var siteIsDark = document.documentElement.classList.contains('dark');
    var themeFollowsSite = true;

    var state = {
      type: 'table',
      limit: 30,
      unit: 'words',
      hideOwner: false,
      tableProgress: 'translation',
      tableLimit: 0,
      tableMinPercent: 0,
      tableLanguages: '',
      overallUnit: 'words',
      overallProgress: 'translation',
      overallMetric: 'both',
      overallVariant: 'card',
      theme: siteIsDark ? 'dark' : 'light',
      colors: Object.assign({}, siteIsDark ? DARK_COLORS : DEFAULT_COLORS),
      // live mode only: raw {languages, contributors} fetched once from
      // /embed/{publicID}/data.json and re-rendered locally on every
      // color/limit/progress/etc. change — see ensureLiveData below.
      liveData: null,
      liveDataKey: null,
    };

    var typeButtons = root.querySelectorAll('[data-embed-type]');
    var contribControls = root.querySelector('[data-builder-contrib-controls]');
    var overallControls = root.querySelector('[data-builder-overall-controls]');
    var tableControls = root.querySelector('[data-builder-table-controls]');
    var limitInput = root.querySelector('[data-builder-limit]');
    var limitValue = root.querySelector('[data-builder-limit-value]');
    var unitSelect = root.querySelector('[data-builder-unit]');
    var hideOwnerInput = root.querySelector('[data-builder-hideowner]');
    var tableProgressSelect = root.querySelector('[data-builder-table-progress]');
    var tableLimitInput = root.querySelector('[data-builder-table-limit]');
    var tableMinPercentInput = root.querySelector('[data-builder-table-minpercent]');
    var tableLanguagesInput = root.querySelector('[data-builder-table-languages]');
    var overallUnitSelect = root.querySelector('[data-builder-overall-unit]');
    var overallProgressSelect = root.querySelector('[data-builder-overall-progress]');
    var overallMetricSelect = root.querySelector('[data-builder-overall-metric]');
    var overallVariantSelect = root.querySelector('[data-builder-overall-variant]');
    var themeButtons = root.querySelectorAll('[data-builder-theme]');
    var colorInputs = root.querySelectorAll('[data-builder-color]');
    var colorHexInputs = root.querySelectorAll('[data-builder-color-hex]');
    var contrastEls = root.querySelectorAll('[data-builder-contrast]');
    var contrastTipEls = root.querySelectorAll('[data-builder-contrast-tip]');
    var previewImg = root.querySelector('[data-builder-preview-img]');
    var previewEl = root.querySelector('[data-builder-preview]');
    var urlEl = root.querySelector('[data-builder-url]');
    var copyBtn = root.querySelector('[data-builder-copy]');
    var previewBlobURL = null;
    var liveFetchInFlight = null;
    var liveFetchToken = 0;

    // Fetches the real Crowdin data exactly once per distinct (unit,
    // hideOwner) combination — the only params that change what Crowdin
    // report gets requested (see server-side handleEmbedData). Every other
    // control (colors, limit, progress, etc.) is rendered from this same
    // fetched data with no further network calls, so tweaking them can never
    // burn the project's refresh-token budget.
    function ensureLiveData() {
      if (mode !== 'live') return;
      var key = state.unit + '|' + state.hideOwner;
      if (state.liveData && state.liveDataKey === key) return;
      if (liveFetchInFlight === key) return;
      liveFetchInFlight = key;
      var token = ++liveFetchToken;
      var qs = 'unit=' + encodeURIComponent(state.unit) + '&hideOwner=' + state.hideOwner;
      fetch(baseEmbedURL + '/data.json?' + qs)
        .then(function (res) {
          if (!res.ok) throw new Error('data fetch failed');
          return res.json();
        })
        .then(function (data) {
          if (token !== liveFetchToken) return;
          liveFetchInFlight = null;
          state.liveData = data;
          state.liveDataKey = key;
          render();
        })
        .catch(function () {
          if (token !== liveFetchToken) return;
          liveFetchInFlight = null;
        });
    }

    function syncColorInputs() {
      colorInputs.forEach(function (input) {
        input.value = state.colors[input.getAttribute('data-builder-color')];
      });
      colorHexInputs.forEach(function (input) {
        input.value = state.colors[input.getAttribute('data-builder-color-hex')];
      });
    }

    function renderContrastWarnings() {
      // Warn when a color that's rendered as text (text/muted/accent) is
      // hard to read against the chosen background — WCAG AA for normal
      // text is a 4.5:1 contrast ratio. The number alone ("4.8:1") means
      // nothing to most people, so it's paired with a plain-language
      // tooltip (see the (i) button next to it) rather than just the
      // ratio and a WCAG citation.
      contrastEls.forEach(function (el) {
        var key = el.getAttribute('data-builder-contrast');
        var ratio = contrastRatio(state.colors[key], state.colors.bg);
        var passes = ratio >= 4.5;
        el.textContent = (passes ? '' : '⚠ ') + ratio.toFixed(1) + ':1';
        el.classList.toggle('text-red-600', !passes);
        el.classList.toggle('dark:text-red-400', !passes);
        el.classList.toggle('text-text-muted', passes);
      });
      contrastTipEls.forEach(function (el) {
        var key = el.getAttribute('data-builder-contrast-tip');
        var ratio = contrastRatio(state.colors[key], state.colors.bg);
        var passes = ratio >= 4.5;
        el.textContent = (passes
          ? 'Easy to read: this color stands out well against the background.'
          : 'Hard to read: this color is too close to the background color, so text in it may be difficult to see — especially for people with low vision.')
          + ' (contrast ratio ' + ratio.toFixed(1) + ':1, ' + (passes ? 'meets' : 'below') + ' the WCAG accessibility guideline of 4.5:1)';
      });
    }

    function render() {
      typeButtons.forEach(function (btn) {
        var active = btn.getAttribute('data-embed-type') === state.type;
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
        btn.classList.toggle('bg-accent', active);
        btn.classList.toggle('text-accent-contrast', active);
        btn.classList.toggle('text-text-muted', !active);
      });
      if (contribControls) {
        contribControls.classList.toggle('hidden', state.type !== 'contributors');
      }
      if (overallControls) {
        overallControls.classList.toggle('hidden', state.type !== 'overall');
      }
      if (tableControls) {
        tableControls.classList.toggle('hidden', state.type !== 'table');
      }
      themeButtons.forEach(function (btn) {
        var active = btn.getAttribute('data-builder-theme') === state.theme;
        btn.setAttribute('aria-pressed', active ? 'true' : 'false');
        btn.classList.toggle('bg-accent', active);
        btn.classList.toggle('text-accent-contrast', active);
        btn.classList.toggle('text-text-muted', !active);
      });

      renderContrastWarnings();

      var qs = buildQueryString(state.type, state);
      var filename = state.type === 'table' ? 'table.svg' : state.type === 'overall' ? 'overall.svg' : 'contributors.svg';
      var fullURL = baseEmbedURL + '/' + filename + qs;
      if (urlEl) {
        if (mode === 'demo') {
          // baseEmbedURL still contains the literal "{public_id}" stand-in
          // here — flag it visually so it doesn't read as a working URL.
          urlEl.innerHTML = fullURL
            .split('{public_id}')
            .map(esc)
            .join('<span class="builder-placeholder">{public_id}</span>');
        } else {
          urlEl.textContent = fullURL;
        }
      }

      if (previewImg || previewEl) {
        ensureLiveData();
        var haveData = mode === 'demo' || !!state.liveData;
        var languages = mode === 'demo' ? DEMO_LANGUAGES : (haveData ? state.liveData.languages : []);
        var contributors = mode === 'demo' ? DEMO_CONTRIBUTORS : (haveData ? state.liveData.contributors : []);

        var svg;
        if (!haveData) {
          var loadingWidth = state.type === 'overall'
            ? (state.overallVariant === 'circle' ? CIRCLE.size : CARD.width)
            : state.type === 'contributors' ? 320 : TABLE.width;
          svg = emptyStateSVG(loadingWidth, 60, 'loading preview…', state.colors);
        } else if (state.type === 'table') {
          var pinned = parseLanguagePins(state.tableLanguages);
          var prepared = prepareTableLanguages(languages, state.tableProgress, state.tableMinPercent, state.tableLimit, pinned);
          svg = renderTableSVG(prepared, state.colors);
        } else if (state.type === 'overall') {
          svg = state.overallVariant === 'circle'
            ? renderOverallCircleSVG(languages, state.overallUnit, state.overallProgress, state.colors)
            : renderOverallCardSVG(languages, state.overallUnit, state.overallMetric, state.overallProgress, state.colors);
        } else {
          svg = renderContributorsSVG(contributors, state.limit, state.colors);
        }
        if (state.type === 'contributors') {
          // Contributor avatars are referenced by external <image href> —
          // browsers sandbox SVGs loaded via <img> (data:, blob:, or a
          // plain URL alike) and drop external resource references inside
          // them, so an <img>-rendered preview would show empty circles.
          // Render inline instead; the avatar grid has almost no text, so
          // the host page's font stack doesn't create a visible drift.
          if (previewImg) previewImg.classList.add('hidden');
          if (previewEl) {
            previewEl.classList.remove('hidden');
            previewEl.innerHTML = svg;
          }
        } else if (previewImg) {
          // Rendered through an <img> (like the real embed) rather than
          // innerHTML, so table/overall previews get the same isolated
          // rendering context as the real embed instead of inheriting the
          // host page's font stack/CSS.
          previewImg.classList.remove('hidden');
          if (previewEl) previewEl.classList.add('hidden');
          var blob = new Blob([svg], { type: 'image/svg+xml' });
          var url = URL.createObjectURL(blob);
          if (previewBlobURL) URL.revokeObjectURL(previewBlobURL);
          previewBlobURL = url;
          previewImg.src = url;
        }
      }
    }

    typeButtons.forEach(function (btn) {
      btn.addEventListener('click', function () {
        state.type = btn.getAttribute('data-embed-type');
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
    if (tableProgressSelect) {
      tableProgressSelect.addEventListener('change', function () {
        state.tableProgress = tableProgressSelect.value;
        render();
      });
    }
    if (tableLimitInput) {
      tableLimitInput.addEventListener('input', function () {
        state.tableLimit = parseInt(tableLimitInput.value, 10) || 0;
        render();
      });
    }
    if (tableMinPercentInput) {
      tableMinPercentInput.addEventListener('input', function () {
        state.tableMinPercent = parseInt(tableMinPercentInput.value, 10) || 0;
        render();
      });
    }
    if (tableLanguagesInput) {
      tableLanguagesInput.addEventListener('input', function () {
        state.tableLanguages = tableLanguagesInput.value;
        render();
      });
    }
    if (overallProgressSelect) {
      overallProgressSelect.addEventListener('change', function () {
        state.overallProgress = overallProgressSelect.value;
        render();
      });
    }
    if (overallUnitSelect) {
      overallUnitSelect.addEventListener('change', function () {
        state.overallUnit = overallUnitSelect.value;
        render();
      });
    }
    if (overallMetricSelect) {
      overallMetricSelect.addEventListener('change', function () {
        state.overallMetric = overallMetricSelect.value;
        render();
      });
    }
    if (overallVariantSelect) {
      overallVariantSelect.addEventListener('change', function () {
        state.overallVariant = overallVariantSelect.value;
        if (overallMetricSelect) overallMetricSelect.disabled = state.overallVariant === 'circle';
        render();
      });
    }
    themeButtons.forEach(function (btn) {
      btn.addEventListener('click', function () {
        themeFollowsSite = false;
        state.theme = btn.getAttribute('data-builder-theme');
        state.colors = Object.assign({}, state.theme === 'dark' ? DARK_COLORS : DEFAULT_COLORS);
        syncColorInputs();
        render();
      });
    });

    colorInputs.forEach(function (input) {
      var key = input.getAttribute('data-builder-color');
      input.value = state.colors[key];
      input.addEventListener('input', function () {
        themeFollowsSite = false;
        state.colors[key] = input.value;
        syncColorInputs();
        render();
      });
    });

    colorHexInputs.forEach(function (input) {
      var key = input.getAttribute('data-builder-color-hex');
      input.value = state.colors[key];
      input.addEventListener('input', function () {
        var normalized = normalizeHex(input.value);
        if (!normalized) return;
        themeFollowsSite = false;
        state.colors[key] = normalized;
        syncColorInputs();
        render();
      });
      input.addEventListener('blur', function () {
        // Revert stray/invalid text back to the last valid color on blur.
        input.value = state.colors[key];
      });
    });

    // Keep the builder's default light/dark palette in sync with the site's
    // own toggle (in <header>) as long as the user hasn't picked a theme or
    // color here themselves.
    new MutationObserver(function () {
      if (!themeFollowsSite) return;
      var dark = document.documentElement.classList.contains('dark');
      state.theme = dark ? 'dark' : 'light';
      state.colors = Object.assign({}, dark ? DARK_COLORS : DEFAULT_COLORS);
      syncColorInputs();
      render();
    }).observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });

    if (copyBtn) {
      if (mode === 'demo') copyBtn.textContent = 'Copy template';
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

  window.CrowdinStatsEmbedBuilder = { init: init };
})();
