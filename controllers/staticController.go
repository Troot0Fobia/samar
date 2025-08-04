package controllers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func GetStaticFile(c *gin.Context) {
	asset_type := c.Param("asset_type")
	filename := c.Param("filename")

	asset_type = strings.ReplaceAll(asset_type, "../", "")
	filename = strings.ReplaceAll(filename, "../", "")

	var asset_path string
	switch asset_type {
	case "icons", "images":
		asset_path = fmt.Sprintf("./views/assets/%s/%s", asset_type, filename)
	case "css", "js":
		asset_path = fmt.Sprintf("./views/%s/%s", asset_type, filename)
	default:
		c.AbortWithStatus(http.StatusBadGateway)
	}
	c.File(asset_path)
}

func GetPolygons(c *gin.Context) {
	c.File("./data/geo/geo_regions.json")
}
