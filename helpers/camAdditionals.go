package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type location struct {
	Country     string `json:"country"`
	Country_rus string `json:"country_rus"`
	Region      string `json:"region"`
	Region_rus  string `json:"region_rus"`
	City        string `json:"city"`
	City_rus    string `json:"city_rus"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

func GetLocation(ip string) (location, error) {
	type result struct {
		Location location
		Error    error
	}

	resChan := make(chan result, 1)
	go func() {
		for i := range 3 {
			LogSuccess(fmt.Sprintf("Attempt #%d for receiving location for ip %s", i, ip), "")
			proxy, err := GetProxy()
			if err != nil {
				LogError("Error receive proxy for ip", "", err)
				continue
			}
			proxyUrl, err := url.Parse(proxy.ProxyUrl)
			if err != nil {
				LogError("Error parse proxy", "", err)
				continue
			}
			tr := &http.Transport{Proxy: http.ProxyURL(proxyUrl)}

			client := &http.Client{Timeout: 10 * time.Second, Transport: tr}
			resp, err := client.Get(fmt.Sprintf("https://api.2ip.ua/geo.json?ip=%s", ip))
			if err_l := UpdateProxyUsageCount(1, proxy.ProxyUrl); err_l != nil {
				LogError("Error updating proxy usage count", "", err_l)
			}
			if err != nil {
				LogError("Error send request", "", err)
				continue
			}

			var resp_json location
			err = json.NewDecoder(resp.Body).Decode(&resp_json)
			resp.Body.Close()
			if err != nil {
				LogError("Error decoding response json", "", err)
				continue
			}
			resp_json.Region = strings.TrimSuffix(resp_json.Region, " oblast")

			resChan <- result{Location: resp_json}
			return
		}
		resChan <- result{Error: errors.New("error with request")}
	}()

	select {
	case res := <-resChan:
		return res.Location, res.Error
	case <-time.After(32 * time.Second):
		return location{}, errors.New("timeout for requests")
	}
}

func ParseCoords(coords string) (float64, float64, error) {
	split_coords := strings.Split(coords, ",")

	lat, err := strconv.ParseFloat(strings.Trim(split_coords[0], ", "), 64)
	if err != nil {
		return 0.0, 0.0, err
	}

	lng, err := strconv.ParseFloat(strings.Trim(split_coords[1], ", "), 64)
	if err != nil {
		return 0.0, 0.0, err
	}

	return lat, lng, nil
}

func GetLastPhotoIndex(baseDir, ip, port string) (int, error) {
	camDir := filepath.Join(baseDir, ip)

	if _, err := os.Stat(camDir); os.IsNotExist(err) {
		return 1, nil
	}

	pattern := fmt.Sprintf(`^%s_%s_(\d+)\.jpg$`, regexp.QuoteMeta(ip), port)
	re := regexp.MustCompile(pattern)

	maxIndex := 0
	err := filepath.WalkDir(camDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		name := d.Name()
		matches := re.FindStringSubmatch(name)
		if len(matches) == 2 {
			idx, _ := strconv.Atoi(matches[1])
			if idx > maxIndex {
				maxIndex = idx
			}
		}
		return nil
	})

	if err != nil {
		return 0, nil
	}

	return maxIndex + 1, nil
}
