let state = {
    namespaces: [],
    allResources: [],
    selectedNamespace: '',
    selectedResource: '',
    objects: [],
    currentObject: null,
    currentFormat: 'yaml',
};

const $ = id => document.getElementById(id);

async function api(path, opts) {
    const res = await fetch('/api' + path, opts);
    if (!res.ok) {
        const text = await res.text();
        throw new Error(text || res.statusText);
    }
    return res.json();
}

// Init
document.addEventListener('DOMContentLoaded', async () => {
    try {
        const status = await api('/status');
        if (status.loaded) {
            onDBLoaded(status.path.split('/').pop());
        }
    } catch (e) {
        // No pre-loaded db
    }
});

// File upload handling
const uploadArea = $('upload-area');
const fileInput = $('file-input');

uploadArea.addEventListener('click', () => fileInput.click());

uploadArea.addEventListener('dragover', e => {
    e.preventDefault();
    uploadArea.classList.add('drag-over');
});

uploadArea.addEventListener('dragleave', () => {
    uploadArea.classList.remove('drag-over');
});

uploadArea.addEventListener('drop', e => {
    e.preventDefault();
    uploadArea.classList.remove('drag-over');
    if (e.dataTransfer.files.length > 0) {
        uploadFile(e.dataTransfer.files[0]);
    }
});

fileInput.addEventListener('change', e => {
    if (e.target.files.length > 0) {
        uploadFile(e.target.files[0]);
    }
});

$('change-db-btn').addEventListener('click', () => {
    showView('upload-view');
    uploadArea.classList.remove('hidden');
    $('upload-progress').classList.add('hidden');
    $('sidebar-placeholder').classList.remove('hidden');
    $('sidebar-content').classList.add('hidden');
    $('db-filename').textContent = 'No database loaded';
    $('change-db-btn').classList.add('hidden');
    $('db-status').textContent = '';
    fileInput.value = '';
});

async function uploadFile(file) {
    $('upload-progress').classList.remove('hidden');
    uploadArea.classList.add('hidden');

    const formData = new FormData();
    formData.append('dbfile', file);

    try {
        const data = await api('/upload', { method: 'POST', body: formData });
        onDBLoaded(data.filename);
    } catch (e) {
        showStatus(e.message, true);
        $('upload-progress').classList.add('hidden');
        uploadArea.classList.remove('hidden');
    }
}

async function onDBLoaded(filename) {
    $('db-filename').textContent = filename;
    $('change-db-btn').classList.remove('hidden');
    showStatus('Database loaded');

    try {
        await loadSidebar();
        showView('welcome');
    } catch (e) {
        showStatus(e.message, true);
    }
}

async function loadSidebar() {
    const [nsData, resData] = await Promise.all([
        api('/namespaces'),
        api('/resources'),
    ]);

    state.namespaces = nsData.namespaces || [];
    state.allResources = resData.resources || [];

    const sel = $('namespace-select');
    sel.innerHTML = '<option value="">All Namespaces</option>';
    state.namespaces.forEach(ns => {
        const opt = document.createElement('option');
        opt.value = ns;
        opt.textContent = ns;
        sel.appendChild(opt);
    });

    renderSidebar(state.allResources);
    $('sidebar-placeholder').classList.add('hidden');
    $('sidebar-content').classList.remove('hidden');

    state.selectedResource = '';
    state.selectedNamespace = '';
}

$('namespace-select').addEventListener('change', async (e) => {
    state.selectedNamespace = e.target.value;
    state.selectedResource = '';
    showView('welcome');

    try {
        if (state.selectedNamespace) {
            const data = await api('/resources?namespace=' + encodeURIComponent(state.selectedNamespace));
            renderSidebar(data.resources || [], true);
        } else {
            renderSidebar(state.allResources);
        }
    } catch (e) {
        showStatus(e.message, true);
    }
});

function renderSidebar(resources, namespacedOnly) {
    const clusterList = $('cluster-resources');
    const namespacedList = $('namespaced-resources');

    if (!namespacedOnly) {
        clusterList.innerHTML = '';
        resources.filter(r => !r.namespaced).sort((a, b) => a.name.localeCompare(b.name)).forEach(r => {
            clusterList.appendChild(makeResourceItem(r));
        });
    }

    namespacedList.innerHTML = '';
    resources.filter(r => r.namespaced).sort((a, b) => a.name.localeCompare(b.name)).forEach(r => {
        namespacedList.appendChild(makeResourceItem(r));
    });
}

function makeResourceItem(r) {
    const li = document.createElement('li');
    li.innerHTML = `<span>${r.name}</span><span class="count">${r.count}</span>`;
    li.addEventListener('click', () => selectResource(r.name, li));
    return li;
}

async function selectResource(resource, element) {
    document.querySelectorAll('.resource-list li').forEach(li => li.classList.remove('active'));
    element.classList.add('active');
    state.selectedResource = resource;

    try {
        let url = '/objects?resource=' + encodeURIComponent(resource);
        if (state.selectedNamespace) {
            url += '&namespace=' + encodeURIComponent(state.selectedNamespace);
        }
        const data = await api(url);
        state.objects = data.objects || [];
        renderObjectList();
    } catch (e) {
        showStatus(e.message, true);
    }
}

function renderObjectList() {
    $('list-title').textContent = state.selectedResource;
    $('list-count').textContent = state.objects.length + ' object(s)';

    const hasNamespace = state.objects.some(obj => obj.namespace);
    const thead = document.querySelector('#object-list thead tr');
    thead.innerHTML = hasNamespace ? '<th>Namespace</th><th>Name</th>' : '<th>Name</th>';

    const tbody = $('objects-tbody');
    tbody.innerHTML = '';

    state.objects.forEach(obj => {
        const tr = document.createElement('tr');
        tr.innerHTML = hasNamespace
            ? `<td>${obj.namespace || '-'}</td><td>${obj.name}</td>`
            : `<td>${obj.name}</td>`;
        tr.addEventListener('click', () => loadObject(obj));
        tbody.appendChild(tr);
    });

    showView('object-list');
}

async function loadObject(obj) {
    try {
        const data = await api('/object?resource=' + encodeURIComponent(state.selectedResource) +
            '&name=' + encodeURIComponent(obj.name) +
            (obj.namespace ? '&namespace=' + encodeURIComponent(obj.namespace) : ''));

        state.currentObject = data;
        state.currentFormat = 'yaml';
        $('format-select').value = 'yaml';
        renderObjectDetail();
    } catch (e) {
        showStatus(e.message, true);
    }
}

function renderObjectDetail() {
    if (!state.currentObject) return;
    const obj = state.currentObject;
    const label = obj.namespace ? obj.resource + '/' + obj.namespace + '/' + obj.name : obj.resource + '/' + obj.name;
    $('detail-title').textContent = label;
    $('object-content').textContent = state.currentFormat === 'yaml' ? obj.yaml : obj.json;
    showView('object-detail');
}

$('back-btn').addEventListener('click', () => showView('object-list'));

$('format-select').addEventListener('change', (e) => {
    state.currentFormat = e.target.value;
    renderObjectDetail();
});

$('copy-btn').addEventListener('click', () => {
    const content = state.currentFormat === 'yaml' ? state.currentObject.yaml : state.currentObject.json;
    navigator.clipboard.writeText(content).then(() => toast('Copied to clipboard'));
});

$('download-btn').addEventListener('click', () => {
    const obj = state.currentObject;
    const ext = state.currentFormat;
    const content = ext === 'yaml' ? obj.yaml : obj.json;
    const filename = (obj.namespace ? obj.namespace + '-' : '') + obj.name + '.' + ext;
    const blob = new Blob([content], {type: 'text/plain'});
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = filename;
    a.click();
    URL.revokeObjectURL(a.href);
});

function showView(view) {
    $('upload-view').classList.add('hidden');
    $('welcome').classList.add('hidden');
    $('object-list').classList.add('hidden');
    $('object-detail').classList.add('hidden');
    $(view).classList.remove('hidden');
}

function showStatus(msg, isError) {
    const el = $('db-status');
    el.textContent = msg;
    el.className = 'db-status' + (isError ? ' error' : '');
}

function toast(msg) {
    const t = document.createElement('div');
    t.className = 'toast';
    t.textContent = msg;
    document.body.appendChild(t);
    setTimeout(() => t.remove(), 2000);
}
