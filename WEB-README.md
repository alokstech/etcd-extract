# etcd-extract Web Interface

A web-based GUI for extracting Kubernetes objects from etcd database files.

## Features

- **Drag & Drop Upload**: Upload etcd database files via drag-and-drop or file picker
- **Resource Browser**: Visual display of all available resources with counts
- **Flexible Filtering**: Filter by resource type, namespace, and object name
- **Multiple Formats**: Export as YAML or JSON
- **Batch Download**: Download all extracted objects as a ZIP file
- **Clean UI**: Modern, responsive interface that works on all devices

## Quick Start

### 1. Build and Run

```bash
# Build the web server
go build -o etcd-extract-web web-server.go

# Run the server
./etcd-extract-web
```

The server will start on `http://localhost:8080`

### 2. Use Custom Port

```bash
PORT=3000 ./etcd-extract-web
```

## How to Use

### Step 1: Upload Database
1. Open `http://localhost:8080` in your browser
2. Click the upload area or drag and drop your etcd database file
3. Wait for the upload to complete

### Step 2: Browse Resources
- View all available resources in your database
- Each card shows:
  - Resource name
  - Type (namespaced or cluster-scoped)
  - Object count
- Click a resource card to auto-select it

### Step 3: Extract Objects
1. Select a resource type from the dropdown
2. (Optional) Enter filters:
   - **Namespace**: Extract objects from a specific namespace
   - **Object Name**: Extract a specific object by name
   - **All Namespaces**: Check to include all namespaces
3. Choose output format (YAML or JSON)
4. Click "Extract Objects"

### Step 4: View and Download
- Results appear in the lower section
- Each object is displayed with its full YAML/JSON content
- Click "Download as ZIP" to download all extracted objects

## API Endpoints

The web server exposes the following REST API:

### `POST /api/upload`
Upload an etcd database file

**Request**: `multipart/form-data` with file field `dbfile`

**Response**:
```json
{
  "sessionId": "abc123...",
  "filename": "db.etcd"
}
```

### `GET /api/resources?sessionId={id}`
List all resources in the uploaded database

**Response**:
```json
{
  "resources": [
    {
      "name": "secrets",
      "type": "namespaced",
      "count": 42,
      "namespaced": true
    }
  ]
}
```

### `POST /api/extract`
Extract objects based on filters

**Request**:
```json
{
  "sessionId": "abc123...",
  "resource": "secrets",
  "namespace": "default",
  "name": "",
  "allNamespaces": false,
  "format": "yaml"
}
```

**Response**:
```json
{
  "objects": [...],
  "count": 5
}
```

### `POST /api/download`
Download extracted objects as a ZIP file

**Request**: Same as `/api/extract`

**Response**: ZIP file download

## Architecture

```
┌─────────────────┐
│   Web Browser   │
│  (HTML/CSS/JS)  │
└────────┬────────┘
         │ HTTP
         ▼
┌─────────────────┐
│   Go Web Server │
│  (web-server.go)│
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  etcd DB File   │
│   (BoltDB)      │
└─────────────────┘
```

### Components

1. **Frontend** (`web/` directory)
   - `index.html`: Main UI
   - `style.css`: Styling
   - `app.js`: JavaScript logic

2. **Backend** (`web-server.go`)
   - File upload handler
   - Session management
   - Resource listing
   - Object extraction
   - ZIP generation

3. **Core Logic** (from `main.go`)
   - BoltDB parsing
   - etcd key parsing
   - Object decoding

## Session Management

- Sessions are created on file upload
- Each session has a unique ID
- Sessions expire after 1 hour
- Uploaded files are automatically cleaned up

## Storage

Uploaded files are stored temporarily in `./uploads/` directory with session-prefixed filenames.

## Security Considerations

- Maximum upload size: 500MB
- File validation: Checks for valid BoltDB format
- Session isolation: Each upload gets a unique session ID
- Auto cleanup: Expired sessions are removed automatically

## Building for Production

### Build with optimizations
```bash
go build -ldflags="-s -w" -o etcd-extract-web web-server.go
```

### Build for different platforms
```bash
# Linux
GOOS=linux GOARCH=amd64 go build -o etcd-extract-web-linux web-server.go

# macOS
GOOS=darwin GOARCH=amd64 go build -o etcd-extract-web-macos web-server.go

# Windows
GOOS=windows GOARCH=amd64 go build -o etcd-extract-web.exe web-server.go
```

## Docker Deployment (Optional)

Create a `Dockerfile`:

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -ldflags="-s -w" -o etcd-extract-web web-server.go

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/etcd-extract-web .
COPY web/ ./web/
RUN mkdir -p ./uploads
EXPOSE 8080
CMD ["./etcd-extract-web"]
```

Build and run:
```bash
docker build -t etcd-extract-web .
docker run -p 8080:8080 etcd-extract-web
```

## Troubleshooting

### Port already in use
```bash
# Use a different port
PORT=3000 ./etcd-extract-web
```

### Upload fails
- Ensure the file is a valid BoltDB database
- Check file size (max 500MB)
- Verify file permissions

### Resources not loading
- Check browser console for errors
- Verify the database file has valid etcd data
- Try the CLI version to validate the database

## Comparison with CLI

| Feature | Web GUI | CLI |
|---------|---------|-----|
| User Interface | Visual, interactive | Command-line |
| Ease of Use | Beginner-friendly | Power users |
| Batch Operations | ZIP download | Shell scripting |
| Remote Access | Yes (web server) | No |
| Resource Usage | Higher (server) | Lower |
| Automation | REST API | Shell scripts |

## License

Same as the main etcd-extract project.
