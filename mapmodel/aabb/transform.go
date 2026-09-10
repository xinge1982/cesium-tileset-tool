package aabb

// === 主函数：WGS84(度/米) → tileset.root.transform（列主序 4x4） ===
func TilesetTransformFromWGS84(lonDeg, latDeg, height float64) [16]float64 {
	lon := deg2rad(lonDeg)
	lat := deg2rad(latDeg)

	// 平移（ECEF 绝对坐标）
	x0, y0, z0 := geodeticToECEF(lon, lat, height)

	// 旋转（把本地 ENU 轴系放到 ECEF）
	R := enuToEcef4(lon, lat)

	// 直接把平移写到最后一列，即等价于 T * R
	R[12], R[13], R[14] = x0, y0, z0
	return R
}
