package main

import (
	"Troot0Fobia/samar/controllers"
	"Troot0Fobia/samar/helpers"
	"Troot0Fobia/samar/initializers"
	"Troot0Fobia/samar/middleware"
	"Troot0Fobia/samar/models"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/gin-gonic/autotls"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func init() {
	initializers.LoadEnvVariables()
	initializers.InitEnv()
	initializers.InitLogger()
	initializers.ConnectToDb()
	initializers.SyncDatabase()
	initializers.LoadGeoJSON()
	// Backfill region keys before seeding so existing regions get keys first.
	// This prevents seed from creating duplicate region records.
	backfillRegionKeys()
	seedRegionsFromGeoJSON()
	seedMaintainers()
}

func seedMaintainers() {
	defaults := []string{"Hikvision", "Dahua"}
	for _, name := range defaults {
		initializers.DB.FirstOrCreate(&models.Maintainer{}, models.Maintainer{Name: name})
	}
}

// seedRegionsFromGeoJSON ensures every GeoJSON-defined region exists in the DB with
// the correct canonical key. Safe to call on every startup (idempotent).
func seedRegionsFromGeoJSON() {
	mainCountry := os.Getenv("MAIN_COUNTRY")
	mainCountryRus := os.Getenv("MAIN_COUNTRY_RUS")
	if mainCountry == "" {
		return
	}
	for _, feature := range initializers.GeoJSON.Features {
		if _, err := helpers.GetOrCreateRegion(
			mainCountry, mainCountryRus,
			feature.Properties.Name,
			feature.Properties.NameRus,
		); err != nil {
			log.Printf("seedRegionsFromGeoJSON: %s: %v", feature.Properties.Name, err)
		}
	}
}

func createAdmin(username string) {
	var exists bool
	if err := initializers.DB.Model(&models.User{}).
		Select("count(*) > 0").
		Where("username = ?", username).
		Find(&exists).Error; err != nil {
		log.Fatalf("DB error: %v", err)
	}
	if exists {
		log.Fatalf("User '%s' already exists", username)
	}

	password := helpers.GeneratePassword(12)
	passHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("Failed to hash password: %v", err)
	}

	if err := initializers.DB.Create(&models.User{
		Username: username,
		PassHash: string(passHash),
		Role:     "admin",
	}).Error; err != nil {
		log.Fatalf("Failed to create admin: %v", err)
	}

	fmt.Printf("Admin created successfully\n")
	fmt.Printf("  Username: %s\n", username)
	fmt.Printf("  Password: %s\n", password)
}

// backfillRegionKeys assigns canonical keys to regions that don't have one yet.
// If a region with the same key already exists (e.g. created by seedRegionsFromGeoJSON),
// the duplicate is merged: cameras and cities are reassigned, the old record deleted.
// Safe to call on every startup — only processes regions with empty keys.
func backfillRegionKeys() {
	var regions []models.Region
	initializers.DB.Where("key = '' OR key IS NULL").Find(&regions)
	if len(regions) == 0 {
		return
	}
	merged, updated := 0, 0
	for _, r := range regions {
		key := helpers.NormalizeToKey(r.Name)
		if key == "" {
			continue
		}
		// Check if another region with this key already exists
		var existing models.Region
		err := initializers.DB.
			Where("key = ? AND country_id = ? AND id != ?", key, r.CountryID, r.ID).
			First(&existing).Error
		if err == nil {
			// Merge inside a transaction: move cameras and cities, then delete the duplicate.
			txErr := initializers.DB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Model(&models.Camera{}).Where("region_id = ?", r.ID).Update("region_id", existing.ID).Error; err != nil {
					return err
				}
				if err := tx.Model(&models.City{}).Where("region_id = ?", r.ID).Update("region_id", existing.ID).Error; err != nil {
					return err
				}
				return tx.Delete(&models.Region{}, r.ID).Error
			})
			if txErr != nil {
				log.Printf("backfillRegionKeys: failed to merge region %d (%s): %v", r.ID, r.Name, txErr)
				continue
			}
			log.Printf("backfillRegionKeys: merged region %d (%s) → %d (%s)", r.ID, r.Name, existing.ID, existing.Name)
			merged++
		} else {
			if err := initializers.DB.Model(&r).Update("key", key).Error; err != nil {
				log.Printf("backfillRegionKeys: region %d (%s): %v", r.ID, r.Name, err)
			} else {
				updated++
			}
		}
	}
	if updated+merged > 0 {
		log.Printf("backfillRegionKeys: updated=%d merged=%d", updated, merged)
	}
}

// camCityGroup holds a canonical city representation and the IDs of cameras that map to it.
type camCityGroup struct {
	key     string
	name    string
	nameRus string
	ids     []uint
}

// legacyCameraRow is used for reading the legacy city/city_rus columns via raw SQL.
// These fields no longer exist on models.Camera but may still be present in the DB
// until drop-city-columns is run.
type legacyCameraRow struct {
	ID       uint
	City     string
	CityRus  string
	RegionID uint
}

// hasColumn reports whether the given column exists in a DB table.
func hasColumn(table, column string) bool {
	var count int64
	initializers.DB.Raw(
		"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
		table, column,
	).Scan(&count)
	return count > 0
}

// groupCamerasByCity deduplicates city names within a slice of legacy camera rows using
// Jaro-Winkler fuzzy matching and returns canonical groups.
// Example: "Hradyz'k" (key "hradyz-k") and "Hradyzk" (key "hradyzk") collapse
// into one group because their Jaro-Winkler similarity exceeds the threshold.
func groupCamerasByCity(rows []legacyCameraRow) []camCityGroup {
	// 0.88 is intentionally looser than geoNormalize's 0.92: migration data has
	// more noise (typos, encoding artifacts) so we tolerate a wider match window.
	const threshold = 0.88
	var groups []camCityGroup

	for _, row := range rows {
		key := helpers.NormalizeToKey(row.City)
		if key == "" {
			continue
		}

		bestIdx := -1
		bestSim := 0.0
		for i, g := range groups {
			if sim := helpers.JaroWinkler(key, g.key); sim > bestSim {
				bestSim = sim
				bestIdx = i
			}
		}

		if bestIdx >= 0 && bestSim >= threshold {
			g := &groups[bestIdx]
			g.ids = append(g.ids, row.ID)
			// Prefer the key with fewest dashes (apostrophe-derived fragments → worse key).
			// E.g. "hradyz-k" has 1 dash, "hradyzk" has 0 — pick "hradyzk".
			curDashes := strings.Count(key, "-")
			bestDashes := strings.Count(g.key, "-")
			if curDashes < bestDashes || (curDashes == bestDashes && len(key) < len(g.key)) {
				g.key = key
				g.name = row.City
				if row.CityRus != "" {
					g.nameRus = row.CityRus
				}
			}
		} else {
			groups = append(groups, camCityGroup{
				key:     key,
				name:    row.City,
				nameRus: row.CityRus,
				ids:     []uint{row.ID},
			})
		}
	}
	return groups
}

// findExistingCity returns a City record for the given region by exact key,
// falling back to Jaro-Winkler fuzzy match against all cities in that region.
func findExistingCity(regionID uint, key string) *models.City {
	var city models.City
	if err := initializers.DB.
		Where("key = ? AND region_id = ?", key, regionID).
		First(&city).Error; err == nil {
		return &city
	}

	const threshold = 0.88
	var candidates []models.City
	initializers.DB.Where("region_id = ?", regionID).Find(&candidates)
	bestSim := 0.0
	var best *models.City
	for i := range candidates {
		if sim := helpers.JaroWinkler(key, candidates[i].Key); sim > bestSim {
			bestSim = sim
			best = &candidates[i]
		}
	}
	if best != nil && bestSim >= threshold {
		return best
	}
	return nil
}

// migrateCities creates City records from the legacy Camera.city strings for cameras
// that don't have a city_id yet, with fuzzy deduplication to prevent duplicate cities.
// Run once after upgrading: ./app migrate-cities
func migrateCities() {
	if !hasColumn("cameras", "city") {
		log.Println("migrate-cities: legacy 'city' column not found — nothing to migrate")
		return
	}

	log.Println("migrate-cities: backfilling region keys and merging duplicates...")
	backfillRegionKeys()

	log.Println("migrate-cities: loading cameras without city_id...")
	var rows []legacyCameraRow
	// city_rus may not yet exist if we're in an intermediate migration state.
	cityRusExpr := "''"
	if hasColumn("cameras", "city_rus") {
		cityRusExpr = "city_rus"
	}
	initializers.DB.Raw(fmt.Sprintf(`
		SELECT id, city, %s AS city_rus, region_id
		FROM cameras
		WHERE city_id IS NULL
		  AND city != ''
		  AND city != 'Unknown'
		  AND deleted_at IS NULL
	`, cityRusExpr)).Scan(&rows)

	if len(rows) == 0 {
		log.Println("migrate-cities: nothing to migrate")
		return
	}
	log.Printf("migrate-cities: found %d cameras to process", len(rows))

	// Group cameras by region_id first, then deduplicate city names within each region.
	byRegion := map[uint][]legacyCameraRow{}
	for _, row := range rows {
		if row.RegionID == 0 {
			log.Printf("migrate-cities: camera %d has no region, skipping", row.ID)
			continue
		}
		byRegion[row.RegionID] = append(byRegion[row.RegionID], row)
	}

	created, reused, migrated, failed := 0, 0, 0, 0

	for regionID, regionRows := range byRegion {
		groups := groupCamerasByCity(regionRows)
		for _, g := range groups {
			existing := findExistingCity(regionID, g.key)

			var cityID uint
			if existing != nil {
				cityID = existing.ID
				reused++
				log.Printf("  region %d: reuse  city '%s' (key=%s) → %d cameras",
					regionID, existing.Name, existing.Key, len(g.ids))
			} else {
				city, err := helpers.GetOrCreateCity(g.name, g.nameRus, regionID)
				if err != nil {
					log.Printf("  region %d: SKIP   city '%s' (key=%s): %v",
						regionID, g.name, g.key, err)
					failed += len(g.ids)
					continue
				}
				cityID = city.ID
				created++
				log.Printf("  region %d: create city '%s' (key=%s) → %d cameras",
					regionID, city.Name, city.Key, len(g.ids))
			}

			result := initializers.DB.Model(&models.Camera{}).
				Where("id IN ?", g.ids).
				Update("city_id", cityID)
			if result.Error != nil {
				log.Printf("  region %d: city %d: update cameras: %v",
					regionID, cityID, result.Error)
				failed += len(g.ids)
				continue
			}
			migrated += int(result.RowsAffected)
		}
	}

	log.Printf("migrate-cities: done — cities created=%d reused=%d cameras migrated=%d failed=%d",
		created, reused, migrated, failed)
}

// clearCityStrings zeroes out the legacy city/city_rus columns for cameras that
// already have a city_id assigned. Run after migrate-cities is complete.
func clearCityStrings() {
	if !hasColumn("cameras", "city") {
		log.Println("clear-city-strings: legacy columns already removed")
		return
	}

	var total int64
	initializers.DB.Raw(`
		SELECT COUNT(*) FROM cameras
		WHERE city_id IS NOT NULL
		  AND (city != '' OR city_rus != '')
		  AND deleted_at IS NULL
	`).Scan(&total)

	if total == 0 {
		log.Println("clear-city-strings: nothing to clear")
		return
	}
	log.Printf("clear-city-strings: clearing city/city_rus from %d cameras...", total)

	if err := initializers.DB.Exec(
		`UPDATE cameras SET city = '', city_rus = '' WHERE city_id IS NOT NULL`,
	).Error; err != nil {
		log.Fatalf("clear-city-strings: %v", err)
	}
	log.Printf("clear-city-strings: done — %d rows cleared", total)
}

// dropCityColumns permanently removes the legacy city and city_rus columns from
// the cameras table. Run only after migrate-cities + clear-city-strings.
func dropCityColumns() {
	migrator := initializers.DB.Migrator()

	// Safety: abort if any camera still has un-migrated city data.
	if hasColumn("cameras", "city") {
		var unmigratedCount int64
		initializers.DB.Raw(`
			SELECT COUNT(*) FROM cameras
			WHERE city_id IS NULL
			  AND city != ''
			  AND city != 'Unknown'
			  AND deleted_at IS NULL
		`).Scan(&unmigratedCount)
		if unmigratedCount > 0 {
			log.Fatalf(
				"drop-city-columns: %d cameras still have city values without city_id — run migrate-cities first",
				unmigratedCount,
			)
		}
	}

	dropped := 0
	for _, col := range []string{"city", "city_rus"} {
		if !hasColumn("cameras", col) {
			log.Printf("drop-city-columns: column '%s' already absent, skipping", col)
			continue
		}
		if err := migrator.DropColumn(&models.Camera{}, col); err != nil {
			log.Fatalf("drop-city-columns: failed to drop '%s': %v", col, err)
		}
		log.Printf("drop-city-columns: dropped column '%s'", col)
		dropped++
	}

	if dropped == 0 {
		log.Println("drop-city-columns: nothing to drop")
	} else {
		log.Printf("drop-city-columns: done — %d columns dropped", dropped)
	}
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "create-admin" {
		createAdmin(os.Args[2])
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "create-admin" {
		log.Fatal("Usage: samar create-admin <username>")
	}
	if len(os.Args) == 2 && os.Args[1] == "migrate-cities" {
		migrateCities()
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "clear-city-strings" {
		clearCityStrings()
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "drop-city-columns" {
		dropCityColumns()
		return
	}

	if initializers.IsDevelopment {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.Default()
	router.SetTrustedProxies(nil)
	router.LoadHTMLFiles("./views/html/map.html", "./views/html/stream.html", "./views/html/cinema.html")

	router.Use(middleware.SecurityHeaders)
	router.Use(middleware.RequestLog)

	router.StaticFile("/favicon.ico", "./views/assets/icons/favicon.ico")
	router.StaticFile("/robots.txt", "./views/robots.txt")
	router.StaticFile("/auth", "./views/html/login.html")
	router.StaticFile("/css/login.css", "./views/css/login.css")
	router.StaticFile("/js/login.js", "./views/js/login.js")
	router.StaticFile("/images/background.mp4", "./views/assets/images/background.mp4")
	router.StaticFile("/assets/icons/open.png", "./views/assets/icons/open.png")
	router.StaticFile("/assets/icons/hide.png", "./views/assets/icons/hide.png")
	router.StaticFile("/assets/icons/copy.png", "./views/assets/icons/copy.png")
	router.StaticFile("/js/stream.js", "./views/js/stream.js")
	router.StaticFile("/css/stream.css", "./views/css/stream.css")

	guestRouter := router.Group("/").Use(middleware.RequireRole(middleware.RoleGuest))
	{
		guestRouter.POST("/auth/login", middleware.LoginLimiter.Handler(), controllers.Login)
		guestRouter.POST("/auth/register", middleware.RegisterLimiter.Handler(), controllers.Signup)
	}

	userRouter := router.Group("/").Use(middleware.RequireRole(middleware.RoleUser))
	{
		userRouter.POST("/auth/logout", controllers.Logout)
		userRouter.GET("/", middleware.GetHomePage)
		userRouter.GET("/cams", controllers.GetCams)
		userRouter.GET("/cam/:ip/:port", controllers.GetCamInfo)
		userRouter.GET("/cam/image/:ip/:image", controllers.GetCamImage)
		userRouter.GET("/cam/polygons", controllers.GetPolygons)
		userRouter.GET("/geo/search", middleware.GeoSearchLimiter.Handler(), controllers.GeoSearch)
		userRouter.GET("/assets/:asset_type/:filename", controllers.GetStaticFile)
		userRouter.GET("/refresh_token", controllers.RefreshToken)

		// Camera live viewer
		userRouter.GET("/stream", controllers.GetStreamPage)
		userRouter.GET("/api/stream/channels/:id", controllers.GetStreamChannels)
		userRouter.POST("/api/stream/open/:id", controllers.StreamOpen)
		userRouter.GET("/api/stream/status/:id", controllers.StreamStatus)
		userRouter.GET("/ws/stream/:id", controllers.WSStream)

		// Recording
		userRouter.POST("/api/record/start/:id", controllers.RecordStart)
		userRouter.POST("/api/record/stop", controllers.RecordStop)
		userRouter.GET("/api/record/list", controllers.RecordList)
		userRouter.GET("/api/record/download/:rec_id", controllers.RecordDownload)

		// Cinema (integrated multi-camera viewer)
		userRouter.GET("/cinema", controllers.GetCinemaPage)
		userRouter.GET("/api/cinema/events", controllers.CinemaEventStream)
		userRouter.GET("/ws/cinema/dahua/:id/:ch", controllers.WsCinemaDahua)
		userRouter.GET("/ws/cinema/rtsp/:id/:chIdx", controllers.WsCinemaRTSP)
		userRouter.GET("/ws/cinema/rtsp/:id", controllers.WsCinemaRTSP)
	}

	moderRouter := router.Group("/cam").Use(middleware.RequireRole(middleware.RoleModer))
	{
		moderRouter.POST("/update_data", controllers.UpdateCamData)
		moderRouter.POST("/define_cam", controllers.DefineCam)
		moderRouter.POST("/change_status", controllers.ChangeStatus)
		moderRouter.POST("/add_camera", controllers.AddCamera)
		moderRouter.POST("/upload_photos", controllers.UploadPhotos)
		moderRouter.DELETE("/delete_photo", controllers.DeletePhoto)
		moderRouter.GET("/cities", controllers.GetCities)
		moderRouter.POST("/add_city", controllers.AddCity)
		moderRouter.GET("/maintainers", controllers.GetMaintainers)
		moderRouter.POST("/add_maintainer", controllers.AddMaintainer)
		moderRouter.GET("/region", controllers.GetRegionByCoords)
		moderRouter.GET("/region_by_ip", controllers.GetRegionByIP)
		moderRouter.DELETE("/delete_cam", controllers.DeleteCamera)
	}

	// Single moder-only route outside /cam prefix — registered inline to avoid a second group.
	router.DELETE("/api/record/:rec_id", middleware.RequireRole(middleware.RoleModer), controllers.RecordDelete)

	adminRouter := router.Group("/admin").Use(middleware.RequireRole(middleware.RoleAdmin))
	{
		adminRouter.POST("/get_token", controllers.GetRegisterToken)
		adminRouter.POST("/upload_cams", controllers.UploadCameras)
	}

	if initializers.IsDevelopment {
		port := os.Getenv("PORT")
		if port == "" {
			port = "4000"
		}
		log.Printf("Running in development mode on http://localhost:%s", port)
		log.Fatal(router.Run(":" + port))
	} else {
		host := os.Getenv("AUTOCERT_HOST")
		if host == "" {
			host = "samar-tour.pro"
		}
		m := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(host, "www."+host),
			Cache:      autocert.DirCache("/home/site_user/.cache"),
		}
		log.Fatal(autotls.RunWithManager(router, &m))
	}
}
