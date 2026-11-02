package road

import (
	"fmt"
	"sort"
	"strings"
)

// Accept adaptive leaves without inventing cells or overriding the database depth.
// Sorted, disjoint cells also give supports and seam faces a deterministic owner.
func roadSliceTargetGeohashes(region [6]float64, precision int, explicit []string) ([]string, error) {
	if explicit == nil {
		return roadSliceGeohashes(region, precision), nil
	}
	hashes := append([]string(nil), explicit...)
	sort.Strings(hashes)
	for i, hash := range hashes {
		if len(hash) < 3 || len(hash) > 11 {
			return nil, fmt.Errorf("road leaf geohash must have length 3..11: %q", hash)
		}
		for _, c := range hash {
			if !strings.ContainsRune("0123456789bcdefghjkmnpqrstuvwxyz", c) {
				return nil, fmt.Errorf("invalid road leaf geohash %q", hash)
			}
		}
		if i > 0 && strings.HasPrefix(hash, hashes[i-1]) {
			return nil, fmt.Errorf("road leaves overlap: %s and %s", hashes[i-1], hash)
		}
	}
	return hashes, nil
}
