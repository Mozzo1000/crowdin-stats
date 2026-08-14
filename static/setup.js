const form = document.getElementById('setup-form');
const submitBtn = document.getElementById('submit-btn');
const errorMsg = document.getElementById('error-msg');
const results = document.getElementById('results');
const tokenInput = document.getElementById('token');
const toggleBtn = document.getElementById('toggle-token');
const eyeIcon = document.getElementById('eye-icon');
const projectSelect = document.getElementById('project-id');
const projectStatus = document.getElementById('project-id-status');
const manualToggle = document.getElementById('manual-project-toggle');
const manualInput = document.getElementById('project-id-manual');
const patInstructions = document.getElementById('pat-instructions');
const termsNotice = document.getElementById('terms-notice');

// Base (theme-less) embed URLs for the "live data" preview grid, set
// once the images are generated. Re-rendered with/without &theme=dark
// whenever the site theme toggles, so these previews track the site's
// theme just like the embed builder below them.
let baseEmbedURLs = null;

// Shows the shimmering placeholder again and fades the <img> out while a
// new src loads, then swaps back once it (or its error) fires — so
// re-renders triggered by a theme toggle get the same loading state as
// the very first load.
function setPreviewLoading(imgId, skeletonId, url) {
  const img = document.getElementById(imgId);
  const skeleton = document.getElementById(skeletonId);
  img.classList.remove('opacity-100');
  img.classList.add('opacity-0');
  skeleton.classList.remove('hidden');
  const reveal = () => {
    img.classList.remove('opacity-0');
    img.classList.add('opacity-100');
    skeleton.classList.add('hidden');
  };
  img.addEventListener('load', reveal, { once: true });
  img.addEventListener('error', reveal, { once: true });
  img.src = url;
}

function renderPreviewImages() {
  if (!baseEmbedURLs) return;
  const dark = document.documentElement.classList.contains('dark');
  const withTheme = (url) => dark ? url + (url.includes('?') ? '&' : '?') + 'theme=dark' : url;

  const tableURL = withTheme(baseEmbedURLs.table);
  const contributorsURL = withTheme(baseEmbedURLs.contributors);
  const overallURL = withTheme(baseEmbedURLs.overall);
  const overallCircleURL = withTheme(baseEmbedURLs.overallCircle);

  setPreviewLoading('preview-table-img', 'preview-table-skeleton', tableURL);
  document.getElementById('markdown-table').textContent = `![Translation Progress](${tableURL})`;
  setPreviewLoading('preview-contrib-img', 'preview-contrib-skeleton', contributorsURL);
  document.getElementById('markdown-contrib').textContent = `![Contributors](${contributorsURL})`;
  setPreviewLoading('preview-overall-img', 'preview-overall-skeleton', overallURL);
  document.getElementById('markdown-overall').textContent = `![Overall](${overallURL})`;
  setPreviewLoading('preview-overall-circle-img', 'preview-overall-circle-skeleton', overallCircleURL);
  document.getElementById('markdown-overall-circle').textContent = `![Overall](${overallCircleURL})`;
}

new MutationObserver(renderPreviewImages)
  .observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });

// Debounce project lookups so we're not hitting the Crowdin API on
// every keystroke while the user is still typing/pasting their token.
const PROJECT_LOOKUP_DEBOUNCE_MS = 600;
let projectLookupTimer = null;
let projectLookupToken = 0; // guards against a stale response clobbering a newer one
let projectMode = 'picker'; // 'picker' | 'manual'

// Some project-scoped Granular Access tokens are allowed to fetch a
// single project (GET /projects/{id}, used at final submit) but are
// rejected by the list endpoint used here. Rather than block onboarding
// on that, manual entry is always one click away and becomes the
// automatic fallback when the picker lookup fails.
function setManualMode(on, statusText) {
  projectMode = on ? 'manual' : 'picker';
  manualInput.classList.toggle('hidden', !on);
  manualInput.disabled = !on;
  projectSelect.classList.toggle('hidden', on);
  projectSelect.disabled = on || projectSelect.options.length <= 1;
  manualToggle.textContent = on ? 'Use project picker instead' : 'Enter ID manually';
  if (statusText !== undefined) {
    projectStatus.textContent = statusText;
    projectStatus.classList.remove('text-red-600', 'dark:text-red-400');
  }
  updateSubmitEnabled();
}

manualToggle.addEventListener('click', () => setManualMode(projectMode !== 'manual'));
manualInput.addEventListener('input', updateSubmitEnabled);

function setProjectOptions(options, placeholder) {
  projectSelect.innerHTML = '';
  const placeholderOpt = document.createElement('option');
  placeholderOpt.value = '';
  placeholderOpt.textContent = placeholder;
  placeholderOpt.disabled = options.length > 0;
  placeholderOpt.selected = options.length === 0;
  projectSelect.appendChild(placeholderOpt);
  options.forEach((opt, i) => {
    const el = document.createElement('option');
    el.value = String(opt.id);
    el.textContent = opt.name + ' (' + opt.id + ')';
    el.selected = i === 0; // auto-select the first project so a single-project token needs no extra click
    projectSelect.appendChild(el);
  });
  projectSelect.disabled = options.length === 0;
  updateSubmitEnabled();
}

function updateSubmitEnabled() {
  const value = projectMode === 'manual' ? manualInput.value.trim() : projectSelect.value;
  submitBtn.disabled = !value;
}

function resetProjectPicker(placeholder) {
  setProjectOptions([], placeholder);
}

async function loadProjects(token) {
  const myLookup = ++projectLookupToken;
  projectStatus.textContent = 'Loading your projects…';
  projectStatus.classList.remove('text-red-600', 'dark:text-red-400');

  let res;
  try {
    res = await fetch('/setup/projects', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    });
  } catch (err) {
    if (myLookup !== projectLookupToken) return;
    resetProjectPicker('Could not load projects');
    setManualMode(true, 'Could not reach the server to load projects — enter the project ID manually.');
    return;
  }

  if (myLookup !== projectLookupToken) return; // token changed again while this was in flight

  if (!res.ok) {
    const text = (await res.text().catch(() => '')) || 'Could not load projects for this token.';
    resetProjectPicker('Could not load projects');
    setManualMode(true, text + ' — enter the project ID manually.');
    return;
  }

  let data;
  try {
    data = await res.json();
  } catch (err) {
    resetProjectPicker('Could not load projects');
    setManualMode(true, 'The server sent back a response we could not understand — enter the project ID manually.');
    return;
  }

  const projects = data.projects || [];
  if (projects.length === 0) {
    resetProjectPicker('No projects found for this token');
    setManualMode(true, 'This token has no accessible projects — enter the project ID manually.');
    return;
  }

  setManualMode(false, 'Select the project this token was scoped to.');
  setProjectOptions(projects, 'Select a project…');
}

tokenInput.addEventListener('input', () => {
  const token = tokenInput.value.trim();
  projectLookupToken++; // invalidate any in-flight/pending lookup for the previous value
  clearTimeout(projectLookupTimer);
  manualInput.value = '';
  setManualMode(false);

  if (!token) {
    resetProjectPicker('Enter your token above first…');
    projectStatus.textContent = '';
    return;
  }

  resetProjectPicker('Waiting for you to finish typing…');
  projectStatus.textContent = '';
  projectLookupTimer = setTimeout(() => loadProjects(token), PROJECT_LOOKUP_DEBOUNCE_MS);
});

projectSelect.addEventListener('change', updateSubmitEnabled);

toggleBtn.addEventListener('click', () => {
  const showing = tokenInput.type === 'text';
  tokenInput.type = showing ? 'password' : 'text';
  toggleBtn.setAttribute('aria-label', showing ? 'Show token' : 'Hide token');
  eyeIcon.innerHTML = showing
    ? '<path d="M1 12s4-7 11-7 11 7 11 7-4 7-11 7-11-7-11-7Z"/><circle cx="12" cy="12" r="3"/>'
    : '<path d="M3 3l18 18M10.6 10.6a3 3 0 0 0 4.24 4.24M9.9 4.24A11 11 0 0 1 12 4c7 0 11 7 11 7a13.2 13.2 0 0 1-3.15 3.9M6.1 6.1A13.3 13.3 0 0 0 1 11s4 7 11 7c1.6 0 3.05-.36 4.32-.94"/>';
});

form.addEventListener('submit', async (e) => {
  e.preventDefault();
  errorMsg.classList.add('hidden');
  submitBtn.disabled = true;
  submitBtn.textContent = 'Verifying...';

  const body = {
    crowdin_project_id: (projectMode === 'manual' ? manualInput.value : projectSelect.value).trim(),
    token: tokenInput.value.trim(),
  };

  let res;
  try {
    res = await fetch('/setup', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    // fetch() itself only throws for a failed connection — DNS, no
    // network, server unreachable, refused, etc. It never throws for
    // an HTTP error status, so this is always a connectivity problem,
    // not something about the submitted token or project ID.
    showError('Could not reach the server. Check your internet connection and try again.');
    return;
  }

  if (!res.ok) {
    let text = '';
    try {
      text = await res.text();
    } catch (err) {
      // response body failed to read; fall through to the generic message below
    }
    showError(text || `Setup failed (server responded with status ${res.status}).`);
    return;
  }

  let data;
  try {
    data = await res.json();
  } catch (err) {
    showError('The server sent back a response we could not understand. Please try again.');
    return;
  }

  form.reset();
  form.classList.add('hidden');
  patInstructions.classList.add('hidden');
  termsNotice.classList.add('hidden');

  baseEmbedURLs = {
    table: data.table_url,
    contributors: data.contributors_url,
    overall: data.overall_url,
    overallCircle: data.overall_url + '&variant=circle',
  };
  renderPreviewImages();
  document.getElementById('revoke-url').value = data.revoke_url;
  results.classList.remove('hidden');
  results.scrollIntoView({ behavior: 'smooth', block: 'start' });
  results.focus({ preventScroll: true });

  CrowdinStatsEmbedBuilder.init(document.querySelector('.embed-builder'), {
    mode: 'live',
    baseEmbedURL: data.embed_base_url,
    projectWebURL: data.project_web_url,
  });

  function showError(message) {
    errorMsg.textContent = message;
    errorMsg.classList.remove('hidden');
    submitBtn.disabled = false;
    submitBtn.textContent = 'Generate images';
  }
});

document.getElementById('copy-revoke-url').addEventListener('click', (e) => {
  const btn = e.currentTarget;
  const original = btn.textContent;
  CrowdinStatsEmbedBuilder.copyToClipboard(document.getElementById('revoke-url').value).then((ok) => {
    btn.textContent = ok ? 'Copied!' : 'Copy failed';
    setTimeout(() => { btn.textContent = original; }, 1500);
  });
});

document.querySelectorAll('.copy-md-btn').forEach((btn) => {
  const original = btn.textContent;
  btn.addEventListener('click', () => {
    const text = document.getElementById(btn.dataset.copyTarget).textContent;
    CrowdinStatsEmbedBuilder.copyToClipboard(text).then((ok) => {
      btn.textContent = ok ? 'Copied!' : 'Copy failed';
      setTimeout(() => { btn.textContent = original; }, 1500);
    });
  });
});
