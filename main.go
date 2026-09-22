package main

//1. 用户名密码鉴权              done
//2. 隐藏/忽略 某文件           done
//3. 隐藏/忽略 某文件夹         done
//4. 正则匹配支持              done
//5. 链接 302 映射            done
//6. 文件断点续传             done
//7. 读取配置文件	            done
//8. WebDVA 只读模式        done
//9. index.html            done

import (
	"bytes"
	"context"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

type FileInfo struct {
	Name      string    `json:"name"`
	Date      string    `json:"date"`
	Size      string    `json:"size"`
	SizeRaw   int64     `json:"-"`
	IsDir     bool      `json:"folder"`
	IsHide    bool      `json:"-"`
	IsIgnore  bool      `json:"-"`
	IsVirtual bool      `json:"-"`
	ParentURL string    `json:"-"`
	URL       string    `json:"url"`
	ModTime   time.Time `json:"-"`
}

type VirtualFile struct {
	Name string
	URL  string
}

type Auth struct {
	User   string
	Passwd string
	Path   string
}

type ViewIndex struct {
	RootPath    string
	CurrentPath string
	RawData     string
}

type Config struct {
	WorkFolder   string
	Endpoint     string
	FolderSize   bool
	AuthItem     string
	RedirectItem string
	IgnoreFile   string
	IgnoreFolder string
	HideFile     string
	HideFolder   string
	WebDAV       bool
}

var (
	BIND              string
	PORT              int
	CONFIG            string
	INDEX             string
	WORK              string
	SUBPATH           string
	BACKGROUND        bool
	QUIET             bool
	IGNORE_File_Raw   string
	HIDE_File_Raw     string
	IGNORE_Folder_Raw string
	HIDE_Folder_Raw   string
	AUTH_ITEM         []Auth
	AUTH_ITEM_Raw     string
	REDIRECT_ITEM     map[string]string
	REDIRECT_ITEM_Raw string
	REDIRECT_File     []FileInfo
	REDIRECT_Root     map[string][]VirtualFile
	HIDEDOT           bool
	DIRSIZE           bool
	WebDAV            bool
	TimeFormat        string
	TimeStart         string
	Template          *template.Template
	HIDE_File_RE      []*regexp.Regexp
	HIDE_Folder_RE    []*regexp.Regexp
	IGNORE_File_RE    []*regexp.Regexp
	IGNORE_Folder_RE  []*regexp.Regexp
	workRealPath      string
	CertFile          string
	KeyFile           string
	IsTLS             bool
)

var templateBufferPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

//go:embed index.html
var embeddedIndexHTML string

const maxPooledBuffer = 1 << 20
const allowedMethodsHeader = "GET, HEAD, OPTIONS, PROPFIND"

func ExecPath() string {
	file, _ := exec.LookPath(os.Args[0])
	path, _ := filepath.Abs(file)
	return filepath.Dir(path)
}

func ExecBackground() {
	if !BACKGROUND {
		return
	}
	switch runtime.GOOS {
	case "linux", "darwin", "freebsd":
	default:
		BACKGROUND = false
	}
}

func InitRedirect() {
	REDIRECT_ITEM = make(map[string]string)
	REDIRECT_Root = make(map[string][]VirtualFile)
	REDIRECT_File = nil
	for _, v := range strings.Split(REDIRECT_ITEM_Raw, "|") {
		if !strings.ContainsRune(v, ';') {
			continue
		}
		item := strings.SplitN(v, ";", 2)
		if len(item) != 2 {
			continue
		}
		m := PathFormat(strings.TrimSpace(item[0]), false)
		u := strings.TrimSpace(item[1])
		if m == "" || m == "/" || u == "" {
			continue
		}
		parsedURL, err := url.Parse(u)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https" && parsedURL.Scheme != "ftp" && parsedURL.Scheme != "ftps") || parsedURL.Host == "" {
			continue
		}
		REDIRECT_ITEM[m] = u
	}
	for k := range REDIRECT_ITEM {
		vf := FileInfo{
			Name:      path.Base(k),
			Date:      TimeStart,
			IsVirtual: true,
			ParentURL: path.Dir(k),
			URL:       REDIRECT_ITEM[k],
			ModTime:   time.Now(),
		}
		REDIRECT_File = append(REDIRECT_File, vf)
		n := path.Base(k)
		r := path.Dir(k)
		for {
			REDIRECT_Root[r] = append(REDIRECT_Root[r], VirtualFile{
				Name: n,
				URL:  path.Join(r, n),
			})
			if r == "/" {
				break
			}
			n = path.Base(r)
			r = path.Dir(r)
		}
	}
}

func InitOption() {
	HIDE_File_RE = compilePatterns(splitOptions(HIDE_File_Raw))
	HIDE_Folder_RE = compilePatterns(splitOptions(HIDE_Folder_Raw))
	IGNORE_File_RE = compilePatterns(splitOptions(IGNORE_File_Raw))
	IGNORE_Folder_RE = compilePatterns(splitOptions(IGNORE_Folder_Raw))
}

func splitOptions(raw string) []string {
	options := make([]string, 0, strings.Count(raw, "|")+1)
	for _, v := range strings.Split(raw, "|") {
		k := strings.TrimSpace(v)
		if k == "" {
			continue
		}
		options = append(options, k)
	}
	return options
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			if !QUIET {
				fmt.Printf("Ignore invalid regular expression %q: %v\n", pattern, err)
			}
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

func InitTemplate() {
	indexExplicitlySet := false
	flag.Visit(func(current *flag.Flag) {
		if current.Name == "i" {
			indexExplicitlySet = true
		}
	})

	if INDEX == "" {
		if indexExplicitlySet {
			// Keep -i "" as the explicit way to disable the web index.
			Template = nil
			return
		}
		INDEX = "index.html"
	}
	if !strings.ContainsRune(INDEX, os.PathSeparator) {
		INDEX = filepath.Join(ExecPath(), INDEX)
	}

	var err error
	if fileExists(INDEX, false) {
		Template, err = template.ParseFiles(INDEX)
	} else if indexExplicitlySet {
		fmt.Printf("Error: Index file not found: %s\n", INDEX)
		os.Exit(1)
	} else {
		INDEX = "<embedded:index.html>"
		Template, err = template.New("index.html").Parse(embeddedIndexHTML)
	}
	if err != nil {
		fmt.Println("Error: Invalid 'index.html' File.")
		os.Exit(1)
	}
}

func InitAuth() {
	AUTH_ITEM = nil
	for _, v := range strings.Split(AUTH_ITEM_Raw, "|") {
		if !strings.ContainsRune(v, ':') {
			continue
		}
		P := "/"
		if strings.ContainsRune(v, '@') {
			k := strings.SplitN(v, "@", 2)
			if len(k) == 2 && strings.TrimSpace(k[1]) != "" {
				P = PathFormat(k[1], false)
			}
			if !strings.ContainsRune(k[0], ':') {
				continue
			}
			v = k[0]
		}
		up := strings.SplitN(v, ":", 2)
		if len(up) != 2 {
			continue
		}
		u := strings.TrimSpace(up[0])
		p := strings.TrimSpace(up[1])
		if u == "" || p == "" {
			continue
		}
		AUTH_ITEM = append(AUTH_ITEM, Auth{
			User:   u,
			Passwd: p,
			Path:   P,
		})
	}
}

func InitConfig() {
	if !(strings.ContainsRune(CONFIG, os.PathSeparator)) {
		CONFIG = filepath.Join(ExecPath(), CONFIG)
	}
	if !fileExists(CONFIG, false) {
		CONFIG = ""
		return
	}
	config := &Config{}
	data, err := os.ReadFile(CONFIG)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	err = json.Unmarshal(data, config)
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if WORK == "" {
		WORK = config.WorkFolder
	}
	if WebDAV {
		WebDAV = config.WebDAV
	}
	if !DIRSIZE {
		DIRSIZE = config.FolderSize
	}
	if AUTH_ITEM_Raw == "" {
		AUTH_ITEM_Raw = config.AuthItem
	}
	if REDIRECT_ITEM_Raw == "" {
		REDIRECT_ITEM_Raw = config.RedirectItem
	}
	if HIDE_File_Raw == "" {
		HIDE_File_Raw = config.HideFile
	}
	if HIDE_Folder_Raw == "" {
		HIDE_Folder_Raw = config.HideFolder
	}
	if IGNORE_File_Raw == "" {
		IGNORE_File_Raw = config.IgnoreFile
	}
	if IGNORE_Folder_Raw == "" {
		IGNORE_Folder_Raw = config.IgnoreFolder
	}
	SUBPATH = PathFormat(config.Endpoint, false)
}

func formatSize(size int64) string {
	if size == 0 {
		return ""
	}
	units := [...]string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	value := float64(size)
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}

func folderSize(ctx context.Context, dirName string) (size int64) {
	err := filepath.WalkDir(dirName, func(_ string, entry os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		size = 0
		if !QUIET && err != context.Canceled {
			fmt.Println(err.Error())
		}
	}
	return
}

func listDirectory(ctx context.Context, dirName string) (folders, files []FileInfo) {
	dir, err := os.ReadDir(dirName)
	if err != nil {
		return nil, nil
	}
	directoryCount := 0
	for _, entry := range dir {
		if entry.IsDir() {
			directoryCount++
		}
	}
	folders = make([]FileInfo, 0, directoryCount)
	files = make([]FileInfo, 0, len(dir)-directoryCount)
	for _, entry := range dir {
		if ctx.Err() != nil {
			return folders, files
		}
		if HIDEDOT && strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		urlPath := filepath.Join(dirName, entry.Name())
		rf, ok := fileInfoFromOS(urlPath, info)
		if !ok {
			continue
		}
		if rf.IsHide || rf.IsIgnore {
			continue
		}
		if rf.IsDir {
			rf.SizeRaw = 0
			if DIRSIZE {
				rf.SizeRaw = folderSize(ctx, urlPath)
				rf.Size = formatSize(rf.SizeRaw)
			}
			folders = append(folders, rf)
		} else {
			files = append(files, rf)
		}
	}
	return folders, files
}

func FileContentType(fs *os.File) (fsType string) {
	buffer := make([]byte, 512)
	if _, err := fs.Seek(0, io.SeekStart); err != nil {
		return "application/octet-stream"
	}
	n, err := io.ReadFull(fs, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		if !QUIET {
			fmt.Println("Error[FileType]:", err.Error())
		}
		return "application/octet-stream"
	}
	_, _ = fs.Seek(0, io.SeekStart)
	return http.DetectContentType(buffer[:n])
}

func serveFile(w http.ResponseWriter, r *http.Request, filePath, fileName string, modTime time.Time, isAttachment bool) (statusCode int) {
	file, err := os.Open(filePath)
	if err != nil {
		if !QUIET {
			fmt.Println("Error[IO]:", err.Error())
		}
		http.Error(w, "Not Found", http.StatusNotFound)
		return http.StatusNotFound
	}
	defer file.Close()
	fileType := mime.TypeByExtension(filepath.Ext(fileName))
	if fileType == "" {
		fileType = FileContentType(file)
	}
	w.Header().Set("Content-Type", fileType)
	if isAttachment {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	}

	// ServeContent implements conditional requests, HEAD and RFC-compliant byte
	// ranges without buffering the file in memory.
	statusCode = http.StatusOK
	if r.Header.Get("Range") != "" {
		statusCode = http.StatusPartialContent
	}
	http.ServeContent(w, r, fileName, modTime, file)
	return statusCode
}

func fileExists(filePath string, isFolder bool) bool {
	file, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	if isFolder {
		return file.IsDir()
	}
	return file.Mode().IsRegular()
}

func fileStats(filePath string) (FileInfo, bool) {
	file, err := os.Stat(filePath)
	if err != nil {
		return FileInfo{}, false
	}
	return fileInfoFromOS(filePath, file)
}

func matchesAny(patterns []*regexp.Regexp, name string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(name) {
			return true
		}
	}
	return false
}

func fileInfoFromOS(filePath string, file os.FileInfo) (FileInfo, bool) {
	fileMode := file.Mode()
	isDir := fileMode.IsDir()
	if !isDir && !fileMode.IsRegular() {
		return FileInfo{}, false
	}
	fileInfo := FileInfo{
		Name:      file.Name(),
		Date:      file.ModTime().Format(TimeFormat),
		IsDir:     isDir,
		ParentURL: filepath.Dir(filePath),
		URL:       filePath,
		ModTime:   file.ModTime(),
	}
	if isDir {
		fileInfo.IsHide = matchesAny(HIDE_Folder_RE, fileInfo.Name)
		fileInfo.IsIgnore = matchesAny(IGNORE_Folder_RE, fileInfo.Name)
	} else {
		fileInfo.IsHide = matchesAny(HIDE_File_RE, fileInfo.Name)
		fileInfo.IsIgnore = matchesAny(IGNORE_File_RE, fileInfo.Name)
		fileInfo.SizeRaw = file.Size()
		fileInfo.Size = formatSize(fileInfo.SizeRaw)
	}
	return fileInfo, true
}

func PathFormat(urlPath string, Escape bool) string {
	pathArray := make([]string, 0)
	head := "/"
	sep := "/"
	if strings.Contains(urlPath, `\`) {
		if strings.Contains(urlPath, `:\`) {
			urlArray := strings.SplitN(urlPath, `:\`, 2)
			head = fmt.Sprintf(`%v:\`, urlArray[0])
			urlPath = urlArray[1]
		}
		urlPath = strings.ReplaceAll(urlPath, `\`, `/`)
		sep = `\`
	}
	for _, v := range strings.Split(urlPath, "/") {
		if strings.TrimFunc(v, func(r rune) bool { return unicode.IsSpace(r) || r == '.' }) == "" {
			continue
		}
		if Escape {
			pathArray = append(pathArray, url.PathEscape(v))
		} else {
			pathArray = append(pathArray, v)
		}
	}
	return head + strings.Join(pathArray, sep)
}

func cleanRequestPath(rawPath string) (string, bool) {
	normalized := strings.ReplaceAll(rawPath, `\`, "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return "", false
		}
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(normalized, "/"))
	return cleaned, true
}

func pathHasPrefix(value, prefix string) bool {
	if prefix == "/" {
		return strings.HasPrefix(value, "/")
	}
	return value == prefix || strings.HasPrefix(value, prefix+"/")
}

func safeFilePath(urlPath string) (string, bool) {
	relative := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	candidate := filepath.Join(WORK, filepath.FromSlash(relative))
	rel, err := filepath.Rel(WORK, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", false
	}

	// EvalSymlinks prevents a symlink inside the served tree from exposing data
	// outside it. A missing target may still be a valid virtual redirect path.
	realCandidate, err := filepath.EvalSymlinks(candidate)
	if err == nil {
		realRel, relErr := filepath.Rel(workRealPath, realCandidate)
		if relErr != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(os.PathSeparator)) || filepath.IsAbs(realRel) {
			return "", false
		}
	} else if !os.IsNotExist(err) {
		return "", false
	}
	return candidate, true
}

func xmlText(value string) string {
	return template.HTMLEscapeString(value)
}

func fileTimes(file FileInfo) (string, string) {
	modified := file.ModTime
	if modified.IsZero() {
		modified, _ = time.ParseInLocation(TimeFormat, file.Date, time.Local)
	}
	if modified.IsZero() {
		modified = time.Now()
	}
	return modified.UTC().Format(time.RFC3339), modified.UTC().Format(http.TimeFormat)
}

func writeXML(writer io.Writer, xmlPath string, files []FileInfo) {
	_, _ = io.WriteString(writer, `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">`)
	for _, file := range files {
		name := file.Name
		if decoded, err := url.PathUnescape(name); err == nil {
			name = decoded
		}
		href := ""
		if strings.HasPrefix(file.URL, "/") {
			href = file.URL
		} else {
			href = path.Join(xmlPath, url.PathEscape(name))
		}
		if file.IsDir && !strings.HasSuffix(href, "/") {
			href += "/"
		}
		created, modified := fileTimes(file)
		status := "HTTP/1.1 200 OK"
		if file.IsVirtual && !file.IsDir {
			status = "HTTP/1.1 302 Found"
		}
		_, _ = io.WriteString(writer, `<D:response><D:href>`+xmlText(href)+`</D:href><D:propstat><D:prop>`)
		_, _ = io.WriteString(writer, `<D:displayname>`+xmlText(name)+`</D:displayname>`)
		if file.IsDir {
			_, _ = io.WriteString(writer, `<D:resourcetype><D:collection/></D:resourcetype>`)
			_, _ = io.WriteString(writer, `<D:quota-used-bytes>`+strconv.FormatInt(file.SizeRaw, 10)+`</D:quota-used-bytes>`)
		} else {
			_, _ = io.WriteString(writer, `<D:resourcetype/><D:getcontentlength>`+strconv.FormatInt(file.SizeRaw, 10)+`</D:getcontentlength>`)
			_, _ = io.WriteString(writer, `<D:getcontenttype>application/octet-stream</D:getcontenttype>`)
		}
		_, _ = io.WriteString(writer, `<D:creationdate>`+created+`</D:creationdate><D:getlastmodified>`+modified+`</D:getlastmodified>`)
		_, _ = io.WriteString(writer, `</D:prop><D:status>`+status+`</D:status></D:propstat></D:response>`)
	}
	_, _ = io.WriteString(writer, `</D:multistatus>`)
}

func buildFileList(folders, files []FileInfo, urlPath string) []FileInfo {
	virtualFolders := make(map[string]VirtualFile, len(REDIRECT_Root[urlPath]))
	for _, item := range REDIRECT_Root[urlPath] {
		virtualFolders[item.Name] = item
	}

	all := make([]FileInfo, 0, len(folders)+len(files)+len(virtualFolders))
	appendPhysical := func(items []FileInfo) {
		for _, item := range items {
			delete(virtualFolders, item.Name)
			if item.IsIgnore {
				continue
			}
			relative, err := filepath.Rel(WORK, item.URL)
			if err != nil {
				continue
			}
			item.Name = url.PathEscape(item.Name)
			item.URL = escapedURLPath(path.Join(SUBPATH, filepath.ToSlash(relative)))
			all = append(all, item)
		}
	}
	appendPhysical(folders)
	appendPhysical(files)

	for _, item := range REDIRECT_File {
		if urlPath == item.ParentURL {
			delete(virtualFolders, item.Name)
			item.Name = url.PathEscape(item.Name)
			all = append(all, item)
		}
	}

	virtualNames := make([]string, 0, len(virtualFolders))
	for name := range virtualFolders {
		virtualNames = append(virtualNames, name)
	}
	sort.Strings(virtualNames)
	for _, name := range virtualNames {
		item := virtualFolders[name]
		all = append(all, FileInfo{
			Name:      url.PathEscape(item.Name),
			Date:      TimeStart,
			IsDir:     true,
			IsVirtual: true,
			ParentURL: urlPath,
			URL:       escapedURLPath(path.Join(SUBPATH, item.URL)),
			ModTime:   time.Now(),
		})
	}
	return all
}

func escapedURLPath(value string) string {
	parts := strings.Split(path.Clean("/"+value), "/")
	for index, part := range parts {
		if part != "" {
			parts[index] = url.PathEscape(part)
		}
	}
	return strings.Join(parts, "/")
}

func authenticate(w http.ResponseWriter, r *http.Request, auth Auth) (ok bool) {
	user, password, supplied := r.BasicAuth()
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(auth.User)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(auth.Passwd)) == 1
	ok = supplied && userOK && passwordOK
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm=" ", charset="UTF-8"`)
		w.WriteHeader(http.StatusUnauthorized)
	}
	return
}

func writeRedirect(w http.ResponseWriter, statusCode int, redirectURL string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Location", redirectURL)
	w.WriteHeader(statusCode)
}

func writeError(w http.ResponseWriter, statusCode int, statusMsg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = io.WriteString(w, statusMsg)
}

func clientAddress(r *http.Request) string {
	clientAddr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientAddr = strings.TrimSpace(r.RemoteAddr)
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" {
		clientAddr = realIP
	}
	if clientAddr == "" {
		clientAddr = "null"
	}
	return clientAddr
}

func allowedMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND":
		return true
	default:
		return false
	}
}

const (
	viewIndex = iota
	viewWebDAV
	viewNone
)

func selectViewMode(r *http.Request) int {
	if Template == nil {
		if WebDAV {
			return viewWebDAV
		}
		return viewNone
	}
	if !WebDAV {
		return viewIndex
	}
	if r.Method == "PROPFIND" {
		return viewWebDAV
	}
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/xml") || strings.HasPrefix(contentType, "text/xml") {
		return viewWebDAV
	}
	return viewIndex
}

func getBuffer() *bytes.Buffer {
	buffer := templateBufferPool.Get().(*bytes.Buffer)
	buffer.Reset()
	return buffer
}

func putBuffer(buffer *bytes.Buffer) {
	if buffer.Cap() <= maxPooledBuffer {
		buffer.Reset()
		templateBufferPool.Put(buffer)
	}
}

func renderIndex(w http.ResponseWriter, files []FileInfo, urlPath string) (bool, error) {
	encoded := getBuffer()
	defer putBuffer(encoded)
	base64Writer := base64.NewEncoder(base64.StdEncoding, encoded)
	if err := json.NewEncoder(base64Writer).Encode(files); err != nil {
		_ = base64Writer.Close()
		return false, err
	}
	if err := base64Writer.Close(); err != nil {
		return false, err
	}

	view := ViewIndex{
		RootPath:    SUBPATH,
		CurrentPath: urlPath,
		RawData:     encoded.String(),
	}
	response := getBuffer()
	defer putBuffer(response)
	if err := Template.Execute(response, &view); err != nil {
		return false, err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(response.Len()))
	_, err := response.WriteTo(w)
	return true, err
}

func Handler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientAddr := clientAddress(r)
	requestPath, clean := cleanRequestPath(r.URL.Path)
	urlPath := requestPath
	if clean && pathHasPrefix(requestPath, SUBPATH) {
		urlPath = strings.TrimPrefix(requestPath, SUBPATH)
		if urlPath == "" {
			urlPath = "/"
		}
	}
	query := r.URL.Query()
	// Direct file URLs download by default. The bundled index uses preview=1
	// only for media/text displayed inside its preview dialog.
	isPreview := query.Get("preview") == "1"
	isAttachment := !isPreview || query.Get("raw") == "1" || query.Get("download") == "1" || query.Get("attachment") == "1"
	method := r.Method
	statusCode := 200
	defer func() {
		if !QUIET {
			fmt.Printf("[%v] [%v] [%v] [%v]:%s  Time:%v\n", startTime.Format("2006/01/02 15:04:05.000"), clientAddr, method, statusCode, urlPath, time.Since(startTime))
		}
	}()
	if !clean || !pathHasPrefix(requestPath, SUBPATH) {
		statusCode = http.StatusNotFound
		writeError(w, statusCode, "Not Found EndPoint! ")
		return
	}
	if !allowedMethod(method) {
		statusCode = http.StatusMethodNotAllowed
		w.Header().Set("Allow", allowedMethodsHeader)
		writeError(w, statusCode, "Method Not Allowed")
		return
	}
	if method == "OPTIONS" {
		statusCode = http.StatusNoContent
		w.Header().Set("Allow", allowedMethodsHeader)
		w.Header().Set("Access-Control-Allow-Methods", allowedMethodsHeader)
		if WebDAV {
			w.Header().Set("DAV", "1")
		}
		w.WriteHeader(statusCode)
		return
	}
	if method == "PROPFIND" && !WebDAV {
		statusCode = http.StatusMethodNotAllowed
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		writeError(w, statusCode, "WebDAV is disabled")
		return
	}
	viewMode := selectViewMode(r)
	auth := true
	for _, v := range AUTH_ITEM {
		if pathHasPrefix(urlPath, v.Path) {
			auth = authenticate(w, r, v)
			break
		}
	}
	if !auth {
		statusCode = 401
		return
	}
	if redirectURL, exists := REDIRECT_ITEM[urlPath]; exists {
		statusCode = 302
		writeRedirect(w, statusCode, redirectURL)
		return
	}
	hasVirtual := len(REDIRECT_Root[urlPath]) != 0
	filePath, safe := safeFilePath(urlPath)
	if !safe {
		statusCode = http.StatusNotFound
		writeError(w, statusCode, "Not Found! ")
		return
	}
	rf, exists := fileStats(filePath)
	if (!exists && !hasVirtual) || rf.IsIgnore {
		statusCode = 404
		writeError(w, statusCode, "Not Found! ")
		return
	}
	if rf.IsDir || (!exists && hasVirtual) {
		if viewMode == viewNone {
			statusCode = 404
			w.WriteHeader(statusCode)
			return
		}
		var folders, files []FileInfo
		if exists {
			folders, files = listDirectory(r.Context(), rf.URL)
		}
		all := buildFileList(folders, files, urlPath)
		if viewMode == viewWebDAV {
			items := all
			if r.Header.Get("Depth") == "0" {
				items = nil
			}
			self := rf
			if !exists {
				self = FileInfo{IsDir: true, IsVirtual: true, Date: TimeStart, ModTime: time.Now()}
			}
			self.Name = path.Base(urlPath)
			if urlPath == "/" {
				self.Name = "/"
			}
			self.URL = escapedURLPath(path.Join(SUBPATH, urlPath))
			items = append([]FileInfo{self}, items...)
			statusCode = http.StatusMultiStatus
			w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
			w.WriteHeader(statusCode)
			writeXML(w, escapedURLPath(path.Join(SUBPATH, urlPath)), items)
			return
		}
		if wrote, err := renderIndex(w, all, urlPath); err != nil {
			if !QUIET {
				fmt.Println("Error[Template]:", err)
			}
			statusCode = http.StatusInternalServerError
			if !wrote {
				writeError(w, statusCode, "Internal Server Error")
			}
		}
		return
	}
	if method == "PROPFIND" {
		self := rf
		self.URL = escapedURLPath(path.Join(SUBPATH, urlPath))
		statusCode = http.StatusMultiStatus
		w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
		w.WriteHeader(statusCode)
		writeXML(w, path.Dir(self.URL), []FileInfo{self})
		return
	}
	// Keep WebDAV-style requests free of browser-oriented attachment headers.
	// The direct-download default applies only to the regular web view.
	statusCode = serveFile(w, r, filePath, rf.Name, rf.ModTime, isAttachment && viewMode != viewWebDAV)
	return
}

func init() {
	flag.StringVar(&BIND, "bind", "127.0.0.1", "Bind address")
	flag.IntVar(&PORT, "port", 8888, "Port")
	flag.StringVar(&INDEX, "i", "", "Index file (defaults to ./index.html beside the executable, then the embedded page).")
	flag.StringVar(&CONFIG, "c", "config.json", "Config file.")
	flag.StringVar(&WORK, "w", "", "Work directory.")
	flag.StringVar(&SUBPATH, "s", "", "Endpoint.")
	flag.StringVar(&AUTH_ITEM_Raw, "a", "", "Auth items.")
	flag.StringVar(&HIDE_Folder_Raw, "hFolder", "", "Hide Folder.")
	flag.StringVar(&HIDE_File_Raw, "hFile", "", "Hide File.")
	flag.StringVar(&IGNORE_Folder_Raw, "iFolder", "", "Ignore Folder.")
	flag.StringVar(&IGNORE_File_Raw, "iFile", "", "Ignore File.")
	flag.StringVar(&CertFile, "cert", "server.cert.pem", "Cert File.")
	flag.StringVar(&KeyFile, "key", "server.key.pem", "Cert Key File.")
	flag.BoolVar(&HIDEDOT, "hide", true, "Hide dot file.")
	flag.BoolVar(&DIRSIZE, "size", false, "Show directory size. (very slow)")
	flag.BoolVar(&WebDAV, "webdav", true, "WebDAV Support.")
	flag.BoolVar(&BACKGROUND, "d", false, "Run in background.")
	flag.BoolVar(&QUIET, "q", false, "Quiet.")
}

func initialize() {
	flag.Parse()
	ExecBackground()
	if BACKGROUND {
		QUIET = true
	}
	InitConfig()
	WORK, _ = filepath.Abs(strings.TrimSpace(WORK))
	if !fileExists(WORK, true) {
		fmt.Println("Invalid work directory.")
		os.Exit(1)
	}
	workRealPath, _ = filepath.EvalSymlinks(WORK)
	if workRealPath == "" {
		workRealPath = WORK
	}
	InitTemplate()
	TimeFormat = "2006/01/02 15:04:05"
	TimeStart = time.Now().Format(TimeFormat)
	SUBPATH = PathFormat(SUBPATH, false)
	InitAuth()
	InitOption()
	InitRedirect()

	if fileExists(CertFile, false) && fileExists(KeyFile, false) {
		IsTLS = true
	}
}

func main() {
	initialize()
	if BACKGROUND && os.Getppid() != 1 {
		execPath, _ := filepath.Abs(os.Args[0])
		CMD := exec.Command(execPath, os.Args[1:]...)
		CMD.Stdin = os.Stdin
		CMD.Stdout = os.Stdout
		CMD.Stderr = os.Stderr
		if err := CMD.Start(); err != nil {
			fmt.Println(err.Error())
			return
		}
		_ = CMD.Process.Release()
		return
	}
	var err error
	var listen net.Listener
	listen, err = net.Listen("tcp", fmt.Sprintf("%s:%v", BIND, PORT))
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	defer listen.Close()
	if !QUIET {
		fmt.Printf("Root: %v\nServer: %v\nEndpoint: %v\nFolderSize: %v\nHideDotFile: %v\nWebDAV: %v\nConfigFile: %v\nIndexFile: %v\n\n", WORK, listen.Addr(), SUBPATH, DIRSIZE, HIDEDOT, WebDAV, CONFIG, INDEX)
	}
	server := &http.Server{
		Handler:           http.HandlerFunc(Handler),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	if IsTLS {
		err = server.ServeTLS(listen, CertFile, KeyFile)
	} else {
		err = server.Serve(listen)
	}
	if err != nil && err != http.ErrServerClosed {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}
