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
	"runtime/debug"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/exp/maps"
)

// 不需要以/结尾
var destAddr string = "http://localhost:8083"

func MainRegister(router *gin.Engine) {

	router.GET("/listTable", admin.ListTableFat)
	router.GET("/exportXlsx", ExportXlsx)
	router.POST("/importXlsx", ImportXlsx)
	router.POST("/execSQL", ExecSQL)

	router.POST("/saveConn", admin.SaveConn)
	router.GET("/delConn", admin.DelConn)
	router.GET("/connBaseTree", admin.ConnBaseTree)
	router.GET("/listConn2", admin.ListConn2)
	router.GET("/showTree", admin.ShowTree)

	router.POST("/saveTree", admin.SaveTree)
	router.GET("/listDirTree", admin.ListDirTree)
	router.GET("/delTreeNode", admin.DelTreeNode)

	router.POST("/login", admin.Login)
	router.POST("/logout", admin.Logout)

	router.POST("/saveRole", admin.SaveRole)
	router.GET("/delRole", admin.DelRole)
	router.GET("/roleList", admin.RoleList)
	router.GET("/roleBaseList", admin.RoleBaseList)
	router.GET("/findUserByRole", admin.FindUserByRole)

	router.GET("/findUser", admin.FindUser)
	router.POST("/saveUser", admin.SaveUser)
	router.GET("/delUser", admin.DelUser)

	router.POST("/saveUserBio", admin.SaveUserBio)

	router.GET("/listBackupData", admin.ListBackupData)
	router.GET("/showBackupData", admin.ShowBackupData)

	router.GET("/sysMode", func(c *gin.Context) {
		utils.WriteJson(c.Writer, map[string]bool{"isRemote": config.Cfg.IsRemote})
	})

	router.GET("/healthCheck", func(c *gin.Context) {
		utils.WriteJson(c.Writer, "")
	})

	router.Any("/ext/", proxy)

	router.Use(hostCheck())
	router.Use(panicMiddleware())
	router.Use(CORSMiddleware())

	// router.NotFoundHandler = &NotFound{}
	// router.PathPrefix("/").Handler(spaHandler{staticPath: "static", indexPath: "index.html"})
	// router.PathPrefix("/").Handler(&notFound{})

	router.NoRoute(notFound())

	log.Println("路由注册完成")
}

// 对外代理的接口注册
func proxy(c *gin.Context) {

	req, _ := http.NewRequest(c.Request.Method, destAddr+c.Request.RequestURI[4:], c.Request.Body)
	defer c.Request.Body.Close()
	*&req.Header = c.Request.Header
	resp, err := http.DefaultClient.Do(req)
	logutils.PanicErr(err)

	maps.Copy(c.Request.Header, resp.Header)
	c.Status(resp.StatusCode)

	_, err2 := io.Copy(c.Writer, resp.Body)
	logutils.PanicErr(err2)
	defer resp.Body.Close()
}

func notFound() gin.HandlerFunc {
	return func(c *gin.Context) {
		idx := strings.Index(c.Request.RequestURI, "?")
		reqPath := c.Request.RequestURI
		if idx != -1 {
			reqPath = c.Request.RequestURI[:idx]
		}
		file, err := utils.Find("static" + reqPath)
		if err != nil || strings.EqualFold("/", reqPath) {
			file, err = utils.Find("static/index.html")
			logutils.PanicErr(err)
		}
		defer file.Close()
		c.Header("Content-Type", mime.TypeByExtension(filepath.Ext(file.Name())))
		c.Header("Content-Encoding", "gzip")
		w2, _ := gzip.NewWriterLevel(c.Writer, 1)
		defer w2.Close()
		io.Copy(w2, bufio.NewReader(file))
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization")
	}
}

// 一定是最后一个引入的
func panicMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				c.Header("content-type", "application/json;charset=UTF-8")
				c.Status(http.StatusInternalServerError)
				c.Writer.Write(utils.ToJsonString(utils.Result{Code: 500, Msg: err}))
				log.Println(string(debug.Stack()))
			}
		}()
	}
}

// 应该是第一个引入
func hostCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !config.Cfg.IsRemote && !slices.ContainsFunc(config.Cfg.AllowedIP, func(allowedIp string) bool {
			return strings.HasPrefix(c.Request.RemoteAddr, allowedIp+":")
		}) {
			c.Writer.Write([]byte("<div style=\"text-align: center;font-size: xxx-large;\">非法 IP</div>"))
			c.Header("content-type", "text/html; charset=utf-8")
			log.Println("非法IP:" + c.Request.RemoteAddr)
			return
		}
	}
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
