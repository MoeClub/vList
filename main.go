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
	IsVirual  bool      `json:"-"`
	ParentURL string    `json:"-"`
	URL       string    `json:"url"`
	ModTime   time.Time `json:"-"`
}

type FileVirual struct {
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
	IGNORE_File       []string
	IGNORE_File_Raw   string
	HIDE_File         []string
	HIDE_File_Raw     string
	IGNORE_Folder     []string
	IGNORE_Folder_Raw string
	HIDE_Folder       []string
	HIDE_Folder_Raw   string
	AUTH_ITEM         *[]Auth
	AUTH_ITEM_Raw     string
	REDIRECT_ITEM     map[string]string
	REDIRECT_ITEM_Raw string
	REDIRECT_File     *[]FileInfo
	REDIRECT_Root     map[string][]FileVirual
	HIDEDOT           bool
	DIRSIZE           bool
	WebDAV            bool
	TimeFormat        string
	TimeStart         string
	HTTPMethod        map[string]bool
	Template          *template.Template
	URLEncodeMap      map[rune]string
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

const maxPooledBuffer = 1 << 20

func ExecPath() string {
	file, _ := exec.LookPath(os.Args[0])
	path, _ := filepath.Abs(file)
	return filepath.Dir(path)
}

func ExecBackground() {
	if BACKGROUND {
		BACKGROUND = false
		for _, v := range []string{"linux", "darwin", "freebsd"} {
			if v == runtime.GOOS {
				BACKGROUND = true
				break
			}
		}
	}
}

func InitHTTPMethod() {
	HTTPMethod = make(map[string]bool, 4)
	HTTPMethod["GET"] = true
	HTTPMethod["HEAD"] = true
	HTTPMethod["OPTIONS"] = true
	HTTPMethod["PROPFIND"] = true
}

func InitRedirect() {
	REDIRECT_ITEM = make(map[string]string, 0)
	REDIRECT_Root = make(map[string][]FileVirual, 0)
	REDIRECT_File = &[]FileInfo{}
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
	for k, _ := range REDIRECT_ITEM {
		vf := FileInfo{
			Name:      filepath.Base(k),
			Date:      TimeStart,
			Size:      "",
			SizeRaw:   0,
			IsDir:     false,
			IsHide:    false,
			IsIgnore:  false,
			IsVirual:  true,
			ParentURL: path.Dir(k),
			URL:       REDIRECT_ITEM[k],
			ModTime:   time.Now(),
		}
		*REDIRECT_File = append(*REDIRECT_File, vf)
		n := path.Base(k)
		r := path.Dir(k)
		for {
			(REDIRECT_Root)[r] = append((REDIRECT_Root)[r], FileVirual{
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

func InitURLEncodeMap() {
	URLEncodeMap = make(map[rune]string, 0)
	URLEncodeMap['!'] = "%21"
	URLEncodeMap['#'] = "%23"
	URLEncodeMap['$'] = "%24"
	URLEncodeMap['&'] = "%26"
}

func InitOption() {
	HIDE_File = make([]string, 0)
	HIDE_Folder = make([]string, 0)
	IGNORE_File = make([]string, 0)
	IGNORE_Folder = make([]string, 0)
	InitOptionRaw(HIDE_File_Raw, &HIDE_File)
	InitOptionRaw(HIDE_Folder_Raw, &HIDE_Folder)
	InitOptionRaw(IGNORE_File_Raw, &IGNORE_File)
	InitOptionRaw(IGNORE_Folder_Raw, &IGNORE_Folder)
	HIDE_File_RE = compilePatterns(HIDE_File)
	HIDE_Folder_RE = compilePatterns(HIDE_Folder)
	IGNORE_File_RE = compilePatterns(IGNORE_File)
	IGNORE_Folder_RE = compilePatterns(IGNORE_Folder)
}

func InitOptionRaw(raw string, list *[]string) {
	for _, v := range strings.Split(raw, "|") {
		k := strings.TrimSpace(v)
		if k == "" {
			continue
		}
		*list = append(*list, k)
	}
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
	if !(strings.ContainsRune(INDEX, os.PathSeparator)) && INDEX != "" {
		INDEX = filepath.Join(ExecPath(), INDEX)
	}
	if !FileExist(INDEX, false) {
		INDEX = ""
		return
	}
	var err error
	Template, err = template.ParseFiles(INDEX)
	if err != nil {
		fmt.Println("Error: Invalid 'index.html' File.")
		os.Exit(1)
	}
	return
}

func InitAuth() {
	AUTH_ITEM = &[]Auth{}
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
		up := strings.Split(v, ":")
		if len(up) != 2 {
			continue
		}
		u := strings.TrimSpace(up[0])
		p := strings.TrimSpace(up[1])
		if u == "" || p == "" {
			continue
		}
		*AUTH_ITEM = append(*AUTH_ITEM, Auth{
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
	if !FileExist(CONFIG, false) {
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
	if WebDAV == true {
		WebDAV = config.WebDAV
	}
	if DIRSIZE == false {
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

func FormatType(filename string) string {
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	return strings.ToLower(ext)
}

func FormatSize(size int64) string {
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

func FolderSizeSumContext(ctx context.Context, dirName string) (size int64) {
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

func FolderSizeSum(dirName string) int64 {
	return FolderSizeSumContext(context.Background(), dirName)
}

func WalkContext(ctx context.Context, dirName string, Folder *[]FileInfo, File *[]FileInfo) {
	dir, err := os.ReadDir(dirName)
	if err != nil {
		return
	}
	for _, entry := range dir {
		if ctx.Err() != nil {
			return
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
				rf.SizeRaw = FolderSizeSumContext(ctx, urlPath)
				rf.Size = FormatSize(rf.SizeRaw)
			}
			*Folder = append(*Folder, rf)
		} else {
			*File = append(*File, rf)
		}
	}
}

func Walk(dirName string, Folder *[]FileInfo, File *[]FileInfo) {
	WalkContext(context.Background(), dirName, Folder, File)
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

func HanderDownload(w http.ResponseWriter, r *http.Request, filePath, fileName string, modTime time.Time, isAttachment bool) (statusCode int) {
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

func FileExist(filePath string, isFolder bool) (ok bool) {
	file, err := os.Stat(filePath)
	if err != nil {
		return false
	}
	if isFolder == true {
		ok = file.IsDir()
	} else {
		ok = file.Mode().IsRegular()
	}
	return
}

func FileStats(filePath string, fileInfo *FileInfo) {
	if fileInfo.IsVirual {
		return
	}
	file, err := os.Stat(filePath)
	if err != nil {
		return
	}
	info, ok := fileInfoFromOS(filePath, file)
	if ok {
		*fileInfo = info
	}
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
		fileInfo.Size = FormatSize(fileInfo.SizeRaw)
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

func XMLEscape(s string) string {
	m := make([]string, 0)
	for _, v := range strings.Split(s, "/") {
		if strings.TrimSpace(v) == "" {
			continue
		}
		for k, d := range URLEncodeMap {
			if strings.ContainsRune(v, k) {
				v = strings.ReplaceAll(v, string(k), d)
			}
		}
		m = append(m, v)
	}
	return "/" + strings.Join(m, "/")
}

func XMLTime(t string, UTC bool) string {
	_t, err := time.Parse("2006-01-02 15:04:05", t)
	if err != nil {
		return t
	}
	if UTC == true {
		return time.Unix(_t.Unix(), 0).Format("2006-01-02T15:04:05Z")
	} else {
		return time.Unix(_t.Unix(), 0).Format("Mon, 2 Jan 2006 15:04:05 GMT")
	}
}

func XMLString(XMLPath string, File *[]FileInfo, xmlData *[]byte) {
	buffer := new(bytes.Buffer)
	writeXML(buffer, XMLPath, *File)
	*xmlData = buffer.Bytes()
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
		if file.IsVirual && !file.IsDir {
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

type Router struct {
	pattern *regexp.Regexp
	handler http.Handler
}

type RegexpHandler struct {
	routes []*Router
}

func (h *RegexpHandler) HandleFunc(pattern *regexp.Regexp, handler func(http.ResponseWriter, *http.Request)) {
	h.routes = append(h.routes, &Router{pattern, http.HandlerFunc(handler)})
}

func (h *RegexpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, route := range h.routes {
		if route.pattern.MatchString(r.URL.Path) {
			route.handler.ServeHTTP(w, r)
			return
		}
	}
	HanderError(w, r, 404, "Not Found EndPoint! ")
}

func HanderList(w http.ResponseWriter, r *http.Request, All *[]FileInfo, Folder *[]FileInfo, File *[]FileInfo, Virual *[]FileInfo, urlPath string) {
	virualFolder := make(map[string]FileVirual)
	if (REDIRECT_Root)[urlPath] != nil && len((REDIRECT_Root)[urlPath]) != 0 {
		for _, v := range (REDIRECT_Root)[urlPath] {
			virualFolder[v.Name] = v
		}
	}
	for k, v := range *Folder {
		delete(virualFolder, v.Name)
		if v.IsIgnore {
			continue
		}
		(*Folder)[k].Name = url.PathEscape(v.Name)
		relative, _ := filepath.Rel(WORK, v.URL)
		(*Folder)[k].URL = escapedURLPath(path.Join(SUBPATH, filepath.ToSlash(relative)))
		*All = append(*All, (*Folder)[k])

	}
	for k, v := range *File {
		delete(virualFolder, v.Name)
		if v.IsIgnore {
			continue
		}
		(*File)[k].Name = url.PathEscape(v.Name)
		relative, _ := filepath.Rel(WORK, v.URL)
		(*File)[k].URL = escapedURLPath(path.Join(SUBPATH, filepath.ToSlash(relative)))
		*All = append(*All, (*File)[k])
	}
	for _, v := range *Virual {
		if urlPath == v.ParentURL {
			delete(virualFolder, v.Name)
			v.Name = url.PathEscape(v.Name)
			*All = append(*All, v)
		}
	}
	virtualNames := make([]string, 0, len(virualFolder))
	for name := range virualFolder {
		virtualNames = append(virtualNames, name)
	}
	sort.Strings(virtualNames)
	for _, name := range virtualNames {
		v := virualFolder[name]
		*All = append(*All, FileInfo{
			Name:      url.PathEscape(v.Name),
			Date:      TimeStart,
			Size:      "",
			SizeRaw:   0,
			IsDir:     true,
			IsHide:    false,
			IsIgnore:  false,
			IsVirual:  true,
			ParentURL: urlPath,
			URL:       escapedURLPath(path.Join(SUBPATH, v.URL)),
			ModTime:   time.Now(),
		})
	}
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

func HanderAuth(w http.ResponseWriter, r *http.Request, auth Auth) (ok bool) {
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

func HanderRedirect(w http.ResponseWriter, r *http.Request, statusCode int, redirectUrl string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Location", fmt.Sprintf("%v", redirectUrl))
	w.WriteHeader(statusCode)
	w.Write([]byte{})
}

func HanderError(w http.ResponseWriter, r *http.Request, statusCode int, statusMsg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write([]byte(statusMsg))
}

func HanderClientAddr(w http.ResponseWriter, r *http.Request) string {
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

func HanderRange(w http.ResponseWriter, r *http.Request) (start int64, end int64) {
	start = 0
	end = 0
	if r.Header["Range"] == nil {
		return
	}
	array := regexp.MustCompile(`bytes=(\d*)-(\d*)`).FindStringSubmatch(r.Header["Range"][0])
	if len(array) != 3 {
		return
	}
	if array[1] != "" {
		num, err := strconv.Atoi(array[1])
		if err != nil {
			num = 0
		}
		start = int64(num)
	}
	if array[2] != "" {
		num, err := strconv.Atoi(array[2])
		if err != nil {
			num = 0
		}
		end = int64(num)
	}
	return
}

func HandlerViewMode(w http.ResponseWriter, r *http.Request) (viewMode int) {
	viewMode = 0
	//0: Index; 1: WebDAV; 2: None
	if INDEX == "" {
		if WebDAV == false {
			viewMode = 2
			return
		} else {
			viewMode = 1
			return
		}
	}
	if WebDAV == false {
		viewMode = 0
		return
	}
	if r.Method == "PROPFIND" {
		return 1
	}
	if r.Header["Content-Type"] != nil {
		contentType := r.Header.Get("Content-Type")
		if strings.HasPrefix(contentType, "application/xml") || strings.HasPrefix(contentType, "text/xml") {
			viewMode = 1
			return
		}
	}
	return
}

func getQueryValue(QueryMap map[string][]string, key string) (value string) {
	if QueryMap[key] != nil {
		return strings.TrimSpace(QueryMap[key][0])
	}
	return ""
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

func renderIndex(w http.ResponseWriter, files []FileInfo, urlPath string) error {
	encoded := getBuffer()
	defer putBuffer(encoded)
	base64Writer := base64.NewEncoder(base64.StdEncoding, encoded)
	if err := json.NewEncoder(base64Writer).Encode(files); err != nil {
		_ = base64Writer.Close()
		return err
	}
	if err := base64Writer.Close(); err != nil {
		return err
	}

	view := ViewIndex{
		RootPath:    SUBPATH,
		CurrentPath: urlPath,
		RawData:     encoded.String(),
	}
	response := getBuffer()
	defer putBuffer(response)
	if err := Template.Execute(response, &view); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(response.Len()))
	_, err := response.WriteTo(w)
	return err
}

func Handler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	clientAddr := HanderClientAddr(w, r)
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
		HanderError(w, r, statusCode, "Not Found EndPoint! ")
		return
	}
	if !HTTPMethod[method] {
		statusCode = http.StatusMethodNotAllowed
		w.Header().Set("Allow", "GET, HEAD, OPTIONS, PROPFIND")
		HanderError(w, r, statusCode, "Method Not Allowed")
		return
	}
	if method == "OPTIONS" {
		statusCode = http.StatusNoContent
		w.Header().Set("Allow", "GET, HEAD, OPTIONS, PROPFIND")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS, PROPFIND")
		if WebDAV {
			w.Header().Set("DAV", "1")
		}
		w.WriteHeader(statusCode)
		return
	}
	if method == "PROPFIND" && !WebDAV {
		statusCode = http.StatusMethodNotAllowed
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		HanderError(w, r, statusCode, "WebDAV is disabled")
		return
	}
	viewMode := HandlerViewMode(w, r)
	auth := true
	for _, v := range *AUTH_ITEM {
		if pathHasPrefix(urlPath, v.Path) {
			auth = HanderAuth(w, r, v)
			break
		}
	}
	if auth == false {
		statusCode = 401
		return
	}
	if redirectURL, exists := REDIRECT_ITEM[urlPath]; exists {
		statusCode = 302
		HanderRedirect(w, r, statusCode, redirectURL)
		return
	}
	hasVirual := len(REDIRECT_Root[urlPath]) != 0
	rf := &FileInfo{
		URL: "",
	}
	filePath, safe := safeFilePath(urlPath)
	if !safe {
		statusCode = http.StatusNotFound
		HanderError(w, r, statusCode, "Not Found! ")
		return
	}
	FileStats(filePath, rf)
	if (rf.URL == "" && !hasVirual) || rf.IsIgnore {
		statusCode = 404
		HanderError(w, r, statusCode, "Not Found! ")
		return
	}
	if rf.IsDir || (rf.URL == "" && hasVirual) {
		if viewMode == 2 {
			statusCode = 404
			w.WriteHeader(statusCode)
			return
		}
		all := &[]FileInfo{}
		d := &[]FileInfo{}
		f := &[]FileInfo{}
		if rf.URL != "" {
			WalkContext(r.Context(), rf.URL, d, f)
		}
		HanderList(w, r, all, d, f, REDIRECT_File, urlPath)
		if viewMode == 1 {
			items := *all
			if r.Header.Get("Depth") == "0" {
				items = nil
			}
			self := *rf
			if self.URL == "" {
				self = FileInfo{IsDir: true, IsVirual: true, Date: TimeStart, ModTime: time.Now()}
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
		if err := renderIndex(w, *all, urlPath); err != nil {
			if !QUIET {
				fmt.Println("Error[Template]:", err)
			}
			statusCode = http.StatusInternalServerError
			if !strings.Contains(err.Error(), "wrote") {
				HanderError(w, r, statusCode, "Internal Server Error")
			}
		}
		return
	}
	if method == "PROPFIND" {
		self := *rf
		self.URL = escapedURLPath(path.Join(SUBPATH, urlPath))
		statusCode = http.StatusMultiStatus
		w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
		w.WriteHeader(statusCode)
		writeXML(w, path.Dir(self.URL), []FileInfo{self})
		return
	}
	// Keep WebDAV-style requests free of browser-oriented attachment headers.
	// The direct-download default applies only to the regular web view.
	statusCode = HanderDownload(w, r, filePath, rf.Name, rf.ModTime, isAttachment && viewMode != 1)
	return
}

func init() {
	flag.StringVar(&BIND, "bind", "127.0.0.1", "Bind address")
	flag.IntVar(&PORT, "port", 8888, "Port")
	flag.StringVar(&INDEX, "i", "index.html", "Index file.")
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
	if !FileExist(WORK, true) {
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
	InitHTTPMethod()
	InitURLEncodeMap()

	if FileExist(CertFile, false) && FileExist(KeyFile, false) {
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
	Regexp := "^" + regexp.QuoteMeta(SUBPATH) + "(?:/|$)"
	if SUBPATH == "/" {
		Regexp = "^/"
	}
	handler := &RegexpHandler{}
	handler.HandleFunc(regexp.MustCompile(Regexp), Handler)
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
		Handler:           handler,
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
