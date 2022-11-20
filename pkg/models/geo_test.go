package models

import (
	"testing"

	"github.com/golang/geo/s2"
	"github.com/stretchr/testify/require"
)

func TestPolygonCovering(t *testing.T) {
	got, err := (&GeoPolygon{
		Vertices: []*LatLngPoint{
			// Stanford
			{
				Lat: 37.427636,
				Lng: -122.170502,
			},
			// NASA Ames
			{
				Lat: 37.408799,
				Lng: -122.064069,
			},
			// Googleplex
			{
				Lat: 37.421265,
				Lng: -122.086504,
			},
		},
	}).CalculateCovering()

	want := s2.CellUnion{
		s2.CellIDFromToken("808fb0ac"),
		s2.CellIDFromToken("808fb744"),
		s2.CellIDFromToken("808fb754"),
		s2.CellIDFromToken("808fb75c"),
		s2.CellIDFromToken("808fb9fc"),
		s2.CellIDFromToken("808fba04"),
		s2.CellIDFromToken("808fba0c"),
		s2.CellIDFromToken("808fba14"),
		s2.CellIDFromToken("808fba1c"),
		s2.CellIDFromToken("808fba5c"),
		s2.CellIDFromToken("808fba64"),
		s2.CellIDFromToken("808fba6c"),
		s2.CellIDFromToken("808fba74"),
		s2.CellIDFromToken("808fba8c"),
		s2.CellIDFromToken("808fbad4"),
		s2.CellIDFromToken("808fbadc"),
		s2.CellIDFromToken("808fbae4"),
		s2.CellIDFromToken("808fbaec"),
		s2.CellIDFromToken("808fbaf4"),
		s2.CellIDFromToken("808fbb2c"),
	}
	require.NoError(t, err)
	require.Equal(t, want, got)
}
