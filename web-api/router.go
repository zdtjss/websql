package webapi

import (
	"bufio"
	"compress/gzip"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	admin "go-web/web-api/admin"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"golang.org/x/exp/maps"
)

// 不需要以/结尾
var destAddr string = "http://localhost:8083"

func MainRegister(router *mux.Router) {

	router.HandleFunc("/listTable", admin.ListTableFat).Methods("GET")
	router.HandleFunc("/exportXlsx", ExportXlsx).Methods("GET")
	router.HandleFunc("/importXlsx", ImportXlsx).Methods("POST")
	router.HandleFunc("/execSQL", ExecSQL).Methods("GET")

	router.HandleFunc("/saveConn", admin.SaveConn).Methods("POST")
	router.HandleFunc("/delConn", admin.DelConn).Methods("GET")
	router.HandleFunc("/connBaseTree", admin.ConnBaseTree).Methods("GET")
	router.HandleFunc("/listConn2", admin.ListConn2).Methods("GET")
	router.HandleFunc("/showTree", admin.ShowTree).Methods("GET")

	router.HandleFunc("/saveTree", admin.SaveTree).Methods("POST")
	router.HandleFunc("/listDirTree", admin.ListDirTree).Methods("GET")
	router.HandleFunc("/delTreeNode", admin.DelTreeNode).Methods("GET")

	router.HandleFunc("/login", admin.Login).Methods("POST")
	router.HandleFunc("/logout", admin.Logout).Methods("POST")

	router.HandleFunc("/saveRole", admin.SaveRole).Methods("POST")
	router.HandleFunc("/delRole", admin.DelRole).Methods("GET")
	router.HandleFunc("/roleList", admin.RoleList).Methods("GET")
	router.HandleFunc("/roleBaseList", admin.RoleBaseList).Methods("GET")
	router.HandleFunc("/findUserByRole", admin.FindUserByRole).Methods("GET")

	router.HandleFunc("/findUser", admin.FindUser).Methods("GET")
	router.HandleFunc("/saveUser", admin.SaveUser).Methods("POST")
	router.HandleFunc("/delUser", admin.DelUser).Methods("GET")

	router.HandleFunc("/sysMode", func(w http.ResponseWriter, r *http.Request) {
		utils.WriteJson(w, map[string]bool{"isRemote": config.IsRemote})
	}).Methods("GET")

	router.HandleFunc("/healthCheck", func(w http.ResponseWriter, r *http.Request) {
		utils.WriteJson(w, "")
	}).Methods("GET")

	router.HandleFunc("/ext/", proxy)

	router.Use(hostCheck)
	router.Use(panicMiddleware)

	// router.NotFoundHandler = &NotFound{}
	// router.PathPrefix("/").Handler(spaHandler{staticPath: "static", indexPath: "index.html"})
	router.PathPrefix("/").Handler(&notFound{})

	log.Println("路由注册完成")
}

// 对外代理的接口注册
func proxy(w http.ResponseWriter, r *http.Request) {

	req, _ := http.NewRequest(r.Method, destAddr+r.RequestURI[4:], r.Body)
	defer r.Body.Close()
	*&req.Header = r.Header
	resp, err := http.DefaultClient.Do(req)
	logutils.PanicErr(err)

	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	_, err2 := io.Copy(w, resp.Body)
	logutils.PanicErr(err2)
	defer resp.Body.Close()
}

type notFound struct {
}

func (n *notFound) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	idx := strings.Index(r.RequestURI, "?")
	reqPath := r.RequestURI
	if idx != -1 {
		reqPath = r.RequestURI[:idx]
	}
	file, err := utils.Find("static" + reqPath)
	if err != nil || strings.EqualFold("/", reqPath) {
		file, err = utils.Find("static/index.html")
		logutils.PanicErr(err)
	}
	defer file.Close()
	w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(file.Name())))
	w.Header().Set("Content-Encoding", "gzip")
	w2, _ := gzip.NewWriterLevel(w, 1)
	defer w2.Close()
	io.Copy(w2, bufio.NewReader(file))
}

// 一定是最后一个引入的
func panicMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				w.Header().Set("content-type", "application/json;charset=UTF-8")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write(utils.ToJsonString(utils.Result{Code: 500, Msg: err}))
			}
		}()
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}

// 应该是第一个引入
func hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !config.IsRemote && !(strings.HasPrefix(r.RemoteAddr, "[::1]:") || strings.HasPrefix(r.RemoteAddr, "127.0.0.1:")) {
			w.Write([]byte("<div style=\"text-align: center;font-size: xxx-large;\">非法 IP</div>"))
			w.Header().Set("content-type", "text/html; charset=utf-8")
			log.Println("非法IP:" + r.RemoteAddr)
			return
		}
		// Call the next handler, which can be another middleware in the chain, or the final handler.
		next.ServeHTTP(w, r)
	})
}

// spaHandler implements the http.Handler interface, so we can use it
// to respond to HTTP requests. The path to the static directory and
// path to the index file within that static directory are used to
// serve the SPA in the given static directory.
type spaHandler struct {
	staticPath string
	indexPath  string
}

// ServeHTTP inspects the URL path to locate a file within the static dir
// on the SPA handler. If a file is found, it will be served. If not, the
// file located at the index path on the SPA handler will be served. This
// is suitable behavior for serving an SPA (single page application).
func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// get the absolute path to prevent directory traversal
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		// if we failed to get the absolute path respond with a 400 bad request
		// and stop
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// prepend the path with the path to the static directory
	path = filepath.Join(h.staticPath, path)

	// check whether a file exists at the given path
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		// file does not exist, serve index.html
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		// if we got an error (that wasn't that the file doesn't exist) stating the
		// file, return a 500 internal server error and stop
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// otherwise, use http.FileServer to serve the static dir
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}
