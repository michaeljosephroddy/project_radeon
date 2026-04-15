package discovery

import "math"

// interestSimilarity returns the cosine similarity of two L2-normalised vectors.
// Both vecs must be normalised, so dot product == cosine similarity.
func interestSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

// haversineKm returns the great-circle distance in kilometres between two coordinates.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	dlat := (lat2 - lat1) * math.Pi / 180
	dlng := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dlng/2)*math.Sin(dlng/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// proximityScore returns an exponential decay score in [0, 1]:
// 1.0 at 0 km, approaching 0 beyond radiusKm.
func proximityScore(lat1, lng1, lat2, lng2, radiusKm float64) float64 {
	return math.Exp(-haversineKm(lat1, lng1, lat2, lng2) / radiusKm)
}

// computeScore combines interest similarity (65%) and proximity (35%).
// radiusKm is the requesting user's preferred discovery radius.
// If either user has no location, only interest similarity is used.
func computeScore(vecA []float64, latA, lngA *float64, vecB []float64, latB, lngB *float64, radiusKm float64) float64 {
	const alpha, beta = 0.65, 0.35
	iScore := interestSimilarity(vecA, vecB)
	if latA == nil || lngA == nil || latB == nil || lngB == nil {
		return alpha * iScore
	}
	return alpha*iScore + beta*proximityScore(*latA, *lngA, *latB, *lngB, radiusKm)
}
