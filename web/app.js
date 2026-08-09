let state = {
    dbPath: '',
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
            $('db-path').value = status.path;
            state.dbPath = status.path;
            showStatus('Database loaded: ' + status.path);
            await loadSidebar();
        }
    } catch (e) {
        // No pre-loaded db, that's fine
    }
});

$('load-btn').addEventListener('click', loadDB);
$('db-path').addEventListener('keydown', e => { if (e.key === 'Enter') loadDB(); });

async function loadDB() {
    const path = $('db-path').value.trim();
    if (!path) return;

    try {
        showStatus('Loading...');
        await api('/load', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({path}),
        });
        state.dbPath = path;
        showStatus('Loaded: ' + path);
        await loadSidebar();
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

    // Populate namespace dropdown
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

    // Reset main content
    showView('welcome');
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
    // Update active state in sidebar
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

    const tbody = $('objects-tbody');
    tbody.innerHTML = '';

    state.objects.forEach(obj => {
        const tr = document.createElement('tr');
        tr.innerHTML = `<td>${obj.namespace || '-'}</td><td>${obj.name}</td>`;
        tr.addEventListener('click', () => loadObject(obj));
        tbody.appendChild(tr);
    });

    showView('object-list');
}

async function loadObject(obj) {
    try {
        let url = '/object?resource=' + encodeURIComponent(obj.namespace ? state.selectedResource : state.selectedResource);
        url += '&name=' + encodeURIComponent(obj.name);
        if (obj.namespace) {
            url += '&namespace=' + encodeURIComponent(obj.namespace);
        }
        url += '&resource=' + encodeURIComponent(state.selectedResource);

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

$('back-btn').addEventListener('click', () => {
    showView('object-list');
});

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
