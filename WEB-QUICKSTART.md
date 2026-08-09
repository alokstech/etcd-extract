# etcd-extract Web GUI - Quick Start

## Get Started in 3 Steps

### 1. Build the Web Server

```bash
make build-web
```

This creates `dist/etcd-extract-web` executable.

### 2. Start the Server

```bash
# Option A: Using make
make run-web

# Option B: Run directly
./dist/etcd-extract-web

# Option C: Custom port
PORT=3000 ./dist/etcd-extract-web
```

### 3. Open in Browser

Navigate to: **http://localhost:8080**

## Using the Web Interface

### Step 1: Upload Database File
- Click the upload area or drag & drop your etcd database file
- Supported files: `.db`, `.etcd` (BoltDB format)
- Max size: 500MB

### Step 2: Browse Resources
- View all resources automatically detected from your database
- Each card shows:
  - Resource name (e.g., `secrets`, `configmaps`)
  - Type: `namespaced` or `cluster-scoped`
  - Count of objects
- Click any card to auto-select that resource

### Step 3: Extract Objects

**Filter Options:**
- **Resource Type**: Required - select what to extract
- **Namespace**: Optional - filter by specific namespace
- **Object Name**: Optional - extract specific object
- **All Namespaces**: Check to include all namespaces
- **Format**: Choose YAML or JSON

Click **"Extract Objects"** to view results.

### Step 4: Download

Click **"Download as ZIP"** to download all extracted objects in a single ZIP file.

## Example Workflows

### Extract All Secrets from Default Namespace
1. Upload database
2. Click "secrets" card or select from dropdown
3. Enter namespace: `default`
4. Click "Extract Objects"

### Extract Specific ConfigMap
1. Upload database
2. Select resource: `configmaps`
3. Enter namespace: `kube-system`
4. Enter name: `coredns`
5. Click "Extract Objects"

### Export All Namespaces
1. Upload database
2. Click "namespaces" card
3. Click "Extract Objects"
4. Download as ZIP

### Get All Resources of a Type
1. Upload database
2. Select resource type (e.g., `deployments`)
3. Check "Include all namespaces"
4. Click "Extract Objects"

## Screenshots

### Upload Screen
```
┌─────────────────────────────────────┐
│  🔧 etcd-extract                    │
│  Extract Kubernetes objects from    │
│  etcd database files                │
├─────────────────────────────────────┤
│  Step 1: Upload etcd Database       │
│  ┌─────────────────────────────┐   │
│  │         ↑                   │   │
│  │   Click to upload or        │   │
│  │   drag and drop             │   │
│  │   etcd database file        │   │
│  └─────────────────────────────┘   │
└─────────────────────────────────────┘
```

### Resources Browser
```
┌─────────────────────────────────────┐
│  Step 2: Available Resources        │
│  ┌───────┐ ┌───────┐ ┌───────┐    │
│  │secrets│ │config │ │deploy │    │
│  │  NS   │ │maps NS│ │ments  │    │
│  │ 42 obj│ │ 15 obj│ │ 8 obj │    │
│  └───────┘ └───────┘ └───────┘    │
└─────────────────────────────────────┘
```

## API Usage (For Integration)

The web server also exposes REST APIs:

### Upload File
```bash
curl -X POST -F "dbfile=@/path/to/db.etcd" \
  http://localhost:8080/api/upload
```

### List Resources
```bash
curl "http://localhost:8080/api/resources?sessionId=YOUR_SESSION_ID"
```

### Extract Objects
```bash
curl -X POST http://localhost:8080/api/extract \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "YOUR_SESSION_ID",
    "resource": "secrets",
    "namespace": "default",
    "allNamespaces": false,
    "format": "yaml"
  }'
```

### Download ZIP
```bash
curl -X POST http://localhost:8080/api/download \
  -H "Content-Type: application/json" \
  -d '{
    "sessionId": "YOUR_SESSION_ID",
    "resource": "secrets",
    "namespace": "default",
    "format": "yaml"
  }' \
  -o extracted.zip
```

## Features

✅ Drag & drop file upload  
✅ Visual resource browser  
✅ Real-time filtering  
✅ YAML & JSON export  
✅ Batch ZIP download  
✅ Session management  
✅ Auto cleanup (1 hour TTL)  
✅ Mobile responsive  
✅ REST API  

## Requirements

- Go 1.19+ (for building)
- Modern web browser (Chrome, Firefox, Safari, Edge)
- ~10MB disk space for binary
- ~500MB disk space for uploaded files (configurable)

## Troubleshooting

### Port 8080 already in use
```bash
# Use different port
PORT=3000 ./dist/etcd-extract-web
```

### Can't access from another machine
```bash
# Server binds to 0.0.0.0:8080 by default
# Open firewall if needed
sudo firewall-cmd --add-port=8080/tcp --permanent
```

### Upload fails
- Check file is valid BoltDB format
- Verify file size < 500MB
- Check disk space in `./uploads/` directory

### Browser shows "Session not found"
- Sessions expire after 1 hour
- Re-upload the database file

## Production Deployment

For production use:

1. **Set custom upload directory**:
   ```go
   // In web-server.go, change:
   const uploadDir = "/var/lib/etcd-extract/uploads"
   ```

2. **Use reverse proxy** (nginx, Caddy):
   ```nginx
   location / {
       proxy_pass http://localhost:8080;
       client_max_body_size 500M;
   }
   ```

3. **Add authentication** (basic auth, OAuth, etc.)

4. **Use systemd** for auto-start:
   ```ini
   [Unit]
   Description=etcd-extract Web Server
   After=network.target

   [Service]
   Type=simple
   User=etcd-extract
   ExecStart=/usr/local/bin/etcd-extract-web
   Restart=always

   [Install]
   WantedBy=multi-user.target
   ```

## Next Steps

- Read full documentation: [WEB-README.md](WEB-README.md)
- Compare with CLI version: [README.md](README.md)
- Build for production: See "Building for Production" section
