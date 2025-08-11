package initializers

import (
	"encoding/json"
	"log"
	"os"
)

type point struct {
	X float64
	Y float64
}

func (p *point) UnmarshalJSON(data []byte) error {
	var coords [2]float64
	if err := json.Unmarshal(data, &coords); err != nil {
		return err
	}
	p.X = coords[0]
	p.Y = coords[1]
	return nil
}

type BBox struct {
	MinX, MinY, MaxX, MaxY float64
}

type Coordinates [][]point

type feature struct {
	Properties struct {
		Name    string `json:"name"`
		NameRus string `json:"name_rus"`
	}
	Geometry struct {
		Coordinates Coordinates `json:"coordinates"`
	} `json:"geometry"`
	BBox BBox `json:"-"`
}

var GeoJSON struct {
	Features []feature `json:"features"`
}

func LoadGeoJSON() {
	content, err := os.Open("./data/geo/geo_regions.json")
	if err != nil {
		log.Fatalf("Error open json file: %v\n", err)
	}
	defer content.Close()

	err = json.NewDecoder(content).Decode(&GeoJSON)
	if err != nil {
		log.Fatalf("Error while parsing json file: %v\n", err)
	}

	for i := range GeoJSON.Features {
		GeoJSON.Features[i].BBox = calcBBox(GeoJSON.Features[i].Geometry.Coordinates)
	}
}

func calcBBox(coordinates Coordinates) (bbox BBox) {
	bbox.MinX, bbox.MinY = coordinates[0][0].X, coordinates[0][0].Y
	bbox.MaxX, bbox.MaxY = bbox.MinX, bbox.MinY

	for _, point := range coordinates[0] {
		if point.X < bbox.MinX {
			bbox.MinX = point.X
		}
		if point.X > bbox.MaxX {
			bbox.MaxX = point.X
		}
		if point.Y < bbox.MinY {
			bbox.MinY = point.Y
		}
		if point.Y > bbox.MaxY {
			bbox.MaxY = point.Y
		}
	}
	return
}
