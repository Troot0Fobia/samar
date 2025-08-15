package main

import (
	"Troot0Fobia/samar/controllers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"log"

	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/acme/autocert"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.InitLogger()
	initializers.ConnectToDb()
	initializers.SyncDatabase()
	initializers.LoadGeoJSON()
}

func main() {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.StaticFile("/favicon.ico", "./views/assets/icons/favicon.ico")
	router.StaticFile("/robots.txt", "./views/robots.txt")
	router.StaticFile("/auth", "./views/html/login.html")
	router.StaticFile("/css/login.css", "./views/css/login.css")
	router.StaticFile("/js/login.js", "./views/js/login.js")
	router.StaticFile("/images/background.mp4", "./views/assets/images/background.mp4")
	router.StaticFile("/assets/icons/open.png", "./views/assets/icons/open.png")
	router.StaticFile("/assets/icons/hide.png", "./views/assets/icons/hide.png")
	router.StaticFile("/assets/icons/copy.png", "./views/assets/icons/copy.png")

	router.LoadHTMLFiles("./views/html/map.html")

	router.Use(middleware.RequestLog)

	guestRouter := router.Group("/").Use(middleware.RequireRole(middleware.RoleGuest))
	{
		guestRouter.POST("/auth/login", controllers.Login)
		guestRouter.POST("/auth/register", controllers.Signup)
	}

	userRouter := router.Group("/").Use(middleware.RequireRole(middleware.RoleUser))
	{
		userRouter.POST("/auth/logout", controllers.Logout)
		userRouter.GET("/", middleware.GetHomePage)
		userRouter.GET("/cams", controllers.GetCams)
		userRouter.GET("/cam/:ip/:port", controllers.GetCamInfo)
		userRouter.GET("/cam/image/:ip/:image", controllers.GetCamImage)
		userRouter.GET("/cam/polygons", controllers.GetPolygons)
		userRouter.GET("/assets/:asset_type/:filename", controllers.GetStaticFile)
	}

	moderRouter := router.Group("/cam").Use(middleware.RequireRole(middleware.RoleModer))
	{
		moderRouter.POST("/save_comment", controllers.SaveComment)
		moderRouter.POST("/define_cam", controllers.DefineCam)
		moderRouter.POST("/change_status", controllers.ChangeStatus)
		moderRouter.POST("/add_camera", controllers.AddCamera)
		moderRouter.POST("/upload_photos", controllers.UploadPhotos)
		moderRouter.DELETE("/delete_photo", controllers.DeletePhoto)
	}

	adminRouter := router.Group("/admin").Use(middleware.RequireRole(middleware.RoleAdmin))
	{
		adminRouter.POST("/get_token", controllers.GetRegisterToken)
		adminRouter.POST("/upload_cams", controllers.UploadCameras)
	}

	m := autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist("samar-tour.pro", "www.samar-tour.pro"),
		Cache:      autocert.DirCache("/home/site_user/.cache"),
	}

	log.Fatal(autotls.RunWithManager(router, &m))
}
