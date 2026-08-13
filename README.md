# safari-dl (Go Port)

A concurrent, high-throughput Go port of the Safari / O'Reilly Learning book & video course downloader with automated EPUB 3 packaging, HTML sanitization, and parallel chunk fetching.

---

## 🚀 Features

- **Concurrent Worker Pool**: Configurable worker pool (1-16 simultaneous workers) fetching chapters and media assets with connection reuse.
- **EPUB 3 Generator**: Builds compliant EPUB files with automated Table of Contents (NCX/NAV), OPF package metadata, and embedded CSS styling.
- **HTML & Asset Sanitizer**: Cleans raw HTML content, rewrites image URLs to local bundle paths, and injects consistent typographic styles.
- **Cookie-Based Authentication**: Seamlessly authenticates via Netscape-format cookies (`orm-jwt`, `csrftoken`).

---

## 📦 Installation & Usage

### Build CLI
```bash
cd cmd/safari-dl
go build -o safari-dl main.go
```

### Download Book as EPUB
```bash
./safari-dl -url "https://learning.oreilly.com/library/view/book-title/9780123456789/" -cookies cookies.txt
```

---

## 🧪 Testing

```bash
go test ./... -v
```

---

## 📄 License
MIT License.
