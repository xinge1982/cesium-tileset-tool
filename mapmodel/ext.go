package mapmodel

import (
	"cesium-tileset-tool/mapmodel/aabb"
	"cesium-tileset-tool/mapmodel/mergeone"
	"errors"
	"strconv"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
)

func (bm *BuildModels) mergeGlb(model *Model, fidx int) error {
	f := -1
	sn := &gltf.Node{}
	if model.ModelNumber() == 1 {
		f = fidx
		relativeTranslation := bm.centerENU.Offset(model.Coords[0][0], model.Coords[0][1], model.Coords[0][2])
		sn.Translation[0] = relativeTranslation[0]
		sn.Translation[1] = relativeTranslation[2]
		sn.Translation[2] = relativeTranslation[1]
		sn.Rotation = aabb.QuatFromHPRDeg(model.R[0][0], model.R[0][1], model.R[0][2])
		sn.Scale = [3]float64{model.S[0][0], model.S[0][2], model.S[0][1]}
		if model.UseWT[0] {
			//已经有 M_target_world M_tile
			//目标是求 M_instance_local = inverse(M_tile) * M_target_world
			//再把 M_instance_local 分解成 T R（四元数）S
			mTile := bm.Transform()
			mTargetWorld := model.WT[0]
			_, t, r, s, err := ComputeInstanceLocalTRS(mTargetWorld, mTile)
			if err != nil {
				return err
			}
			sn.Translation[0] = t.X
			sn.Translation[1] = t.Z
			sn.Translation[2] = -t.Y
			sn.Rotation = [4]float64{r.X, r.Z, -r.Y, r.W}
			sn.Scale = [3]float64{s.X, s.Z, s.Y}
		}

	} else if model.ModelNumber() > 1 {

		T := make([][3]float32, model.ModelNumber())
		R := make([][4]float32, model.ModelNumber())
		S := make([][3]float32, model.ModelNumber())
		F := make([]uint32, model.ModelNumber())

		for i := range model.Coords {
			tt := f32(bm.centerENU.Offset(model.Coords[i][0], model.Coords[i][1], model.Coords[i][2]))

			T[i] = [3]float32{tt[0], tt[2], tt[1]}
			F[i] = uint32(fidx)
			R[i] = f32_4(aabb.QuatFromHPRDeg(model.R[i][0], model.R[i][1], model.R[i][2]))
			S[i] = f32([3]float64{model.S[i][0], model.S[i][2], model.S[i][1]})
			if model.UseWT[i] {
				mTile := bm.Transform()
				mTargetWorld := model.WT[i]
				_, t, r, s, err := ComputeInstanceLocalTRS(mTargetWorld, mTile)
				if err != nil {
					return err
				}
				T[i] = [3]float32{float32(t.X), float32(t.Z), float32(-t.Y)}
				R[i] = [4]float32{float32(r.X), float32(r.Z), float32(-r.Y), float32(r.W)}
				S[i] = [3]float32{float32(s.X), float32(s.Z), float32(s.Y)}
			}
			fidx++
		}
		//fmt.Println("f:", F, T, R)

		accT := modeler.WriteAccessor(bm.doc, gltf.TargetArrayBuffer, T)
		accR := modeler.WriteAccessor(bm.doc, gltf.TargetArrayBuffer, R)
		accS := modeler.WriteAccessor(bm.doc, gltf.TargetArrayBuffer, S)
		accF := modeler.WriteAccessor(bm.doc, gltf.TargetArrayBuffer, F)

		sn.Extensions = map[string]any{
			"EXT_mesh_gpu_instancing": map[string]any{
				"attributes": map[string]any{
					"TRANSLATION":   accT,
					"ROTATION":      accR,
					"SCALE":         accS,
					"_FEATURE_ID_0": accF,
				},
			},
			"EXT_instance_features": map[string]any{
				"featureIds": []map[string]any{
					{
						"attribute":     0, // 对应 _FEATURE_ID_0
						"propertyTable": 0, // ✅ 统一放表 0，最稳
					},
				},
			},
		}
	} else {
		return errors.New("model is 0")
	}
	sn.Name = strconv.Itoa(fidx)

	_, _, err := mergeone.AppendDocBFlattenedIntoDocA(bm.doc, model.Doc, sn, mergeone.MergeOptions{Fidx: f})

	return err
}
func f32(f [3]float64) [3]float32 {
	return [3]float32{float32(f[0]), float32(f[1]), float32(f[2])}
}
func f32_4(f [4]float64) [4]float32 {
	return [4]float32{float32(f[0]), float32(f[1]), float32(f[2]), float32(f[3])}
}
