package helpers

import "Troot0Fobia/samar/initializers"

func DetectPolygonByPoint(x, y float64) (string, string) {
	for _, feature := range initializers.GeoJSON.Features {
		if isPointInBBox(x, y, feature.BBox) && isPointInPolygon(x, y, feature.Geometry.Coordinates) {
			return feature.Properties.Name, feature.Properties.NameRus
		}
	}
	return "", ""
}

func isPointInPolygon(x, y float64, coordinates initializers.Coordinates) bool {
	points := coordinates[0]
	if len(points) < 3 {
		return false
	}

	inPolygon := false
	j := len(points) - 1
	for i := 0; i < len(points); i++ {
		xi, yi := points[i].X, points[i].Y
		xj, yj := points[j].X, points[j].Y

		if ((yi > y) != (yj > y)) && (x < (xj-xi)*(y-yi)/(yj-yi)+xi) {
			inPolygon = !inPolygon
		}
		j = i
	}

	return inPolygon
}

func isPointInBBox(x, y float64, bbox initializers.BBox) bool {
	return x < bbox.MaxX && x > bbox.MinX && y < bbox.MaxY && y > bbox.MinY
}
