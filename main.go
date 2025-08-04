package main

import (
	"Troot0Fobia/samar/controllers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"log"
	"net/http"

	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/acme/autocert"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.ConnectToDb()
	initializers.SyncDatabase()
}

func main() {
	router := gin.Default()
	router.StaticFile("/favicon.ico", "./views/assets/icons/favicon.ico")
	router.StaticFile("/css/login.css", "./views/css/login.css")
	router.StaticFile("/js/login.js", "./views/js/login.js")
	router.StaticFile("/images/background.mp4", "./views/assets/images/background.mp4")
	router.StaticFile("/assets/icons/open.png", "./views/assets/icons/open.png")
	router.StaticFile("/assets/icons/hide.png", "./views/assets/icons/hide.png")
	router.StaticFile("/assets/icons/copy.png", "./views/assets/icons/copy.png")

	router.LoadHTMLFiles("./views/html/map.html")

	router.Use(middleware.AccessControl())

	router.GET("/auth", controllers.NoCahceHTML, GetAuthPage)
	router.GET("/", controllers.NoCahceHTML, GetHomePage)
	router.GET("/cams", controllers.GetCams)
	router.GET("/cam/:ip/:port", controllers.GetCamInfo)
	router.POST("/cam/save_comment", controllers.SaveComment)
	router.POST("/cam/define_cam", controllers.DefineCam)
	router.POST("/cam/upload_cams", controllers.UploadCameras)
	router.GET("/cam/image/:ip/:image", controllers.GetCamImage)
	router.GET("/cam/polygons", controllers.GetPolygons)
	router.GET("/assets/:asset_type/:filename", controllers.GetStaticFile)

	router.POST("/admin/get_token", controllers.GetRegisterToken)
	router.POST("/auth/login", controllers.Login)
	router.POST("/auth/register", controllers.Signup)
	router.POST("/auth/logout", controllers.Logout)

	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist("samar-tour.pro"),
		Cache:      autocert.DirCache("/home/site_user/.cache"),
	}

	log.Fatal(autotls.RunWithManager(router, &m))

	router.Run()
}

func GetHomePage(c *gin.Context) {
	_, role := middleware.CheckAuth(c)
	c.HTML(http.StatusOK, "map.html", gin.H{"isAdmin": role == "admin"})
}

func GetAuthPage(c *gin.Context) {
	c.File("./views/html/login.html")
}
