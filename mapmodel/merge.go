package mapmodel

import (
	"github.com/qmuntal/gltf"
	log "github.com/sirupsen/logrus"
)

type (
	GltfMerger struct {
		doc           *gltf.Document
		NbAccessors   int
		NbAnimations  int
		NbBuffers     int
		NbBufferViews int
		NbCameras     int
		NbImages      int
		NbMaterials   int
		NbMeshes      int
		NbNodes       int
		NbSamplers    int
		NbScenes      int
		NbSkins       int
		NbTextures    int
	}
)

func NewGltfMerger(doc *gltf.Document) *GltfMerger {
	var merger GltfMerger
	merger.load(doc)
	return &merger
}

func (e *GltfMerger) load(doc *gltf.Document) {
	e.doc = doc
	e.NbAccessors = len(doc.Accessors)
	e.NbAnimations = len(doc.Animations)
	e.NbBuffers = len(doc.Buffers)
	e.NbBufferViews = len(doc.BufferViews)
	e.NbCameras = len(doc.Cameras)
	e.NbImages = len(doc.Images)
	e.NbMaterials = len(doc.Materials)
	e.NbMeshes = len(doc.Meshes)
	e.NbNodes = len(doc.Nodes)
	e.NbSamplers = len(doc.Samplers)
	e.NbScenes = len(doc.Scenes)
	e.NbSkins = len(doc.Skins)
	e.NbTextures = len(doc.Textures)

}

func (e *GltfMerger) WriteDoc(path string) {
	var err error

	if err = gltf.Save(e.doc, path); err != nil {
		log.Error(err)
	}
}

func (e *GltfMerger) Merge(doc *gltf.Document) {
	e.mergeScenes(doc)
	e.mergeNodes(doc)
	e.mergeBuffers(doc)
	e.mergeViewBuffers(doc)
	e.mergeAccessors(doc)
	e.mergeMeshes(doc)
	e.mergeSkins(doc)
	e.mergeTextures(doc)
	e.mergeImages(doc)
	e.mergeSamplers(doc)
	e.mergeMaterials(doc)
	e.mergeCameras(doc)
}

// mergeScenes ignore all the scenes of the document we want to merge
// it will append the new doc nodes in the first scene of our actual document
func (e *GltfMerger) mergeScenes(doc *gltf.Document) {
	for _, scene := range doc.Scenes {
		for _, nodeVal := range scene.Nodes {
			e.doc.Scenes[0].Nodes = append(e.doc.Scenes[0].Nodes, e.NbNodes+nodeVal)
		}
	}
}

// mergeNodes update the index values of meshes, cameras, skins and children
func (e *GltfMerger) mergeNodes(doc *gltf.Document) {
	for _, node := range doc.Nodes {
		if node.Mesh != nil {
			*node.Mesh += e.NbMeshes
		}
		if node.Camera != nil {
			*node.Camera += e.NbCameras
		}
		if node.Skin != nil {
			*node.Skin += e.NbSkins
		}
		for idx := range node.Children {
			node.Children[idx] += e.NbNodes
		}
		e.doc.Nodes = append(e.doc.Nodes, node)
	}
}
func (e *GltfMerger) mergeBuffers(doc *gltf.Document) {
	e.doc.Buffers = append(e.doc.Buffers, doc.Buffers...)
}
func (e *GltfMerger) mergeViewBuffers(doc *gltf.Document) {
	for _, bufferView := range doc.BufferViews {
		bufferView.Buffer += e.NbBuffers
		e.doc.BufferViews = append(e.doc.BufferViews, bufferView)
	}
}
func (e *GltfMerger) mergeAccessors(doc *gltf.Document) {
	for _, accessor := range doc.Accessors {
		if accessor.BufferView != nil {
			*accessor.BufferView += e.NbBufferViews
		}
		if accessor.Sparse != nil {
			accessor.Sparse.Indices.BufferView += e.NbBufferViews
			accessor.Sparse.Values.BufferView += e.NbBufferViews
		}
		e.doc.Accessors = append(e.doc.Accessors, accessor)
	}
}

func (e *GltfMerger) mergeMeshes(doc *gltf.Document) {
	for _, mesh := range doc.Meshes {
		for idx := range mesh.Primitives {
			for field := range mesh.Primitives[idx].Attributes {
				mesh.Primitives[idx].Attributes[field] += e.NbAccessors
			}
			if mesh.Primitives[idx].Indices != nil {
				*mesh.Primitives[idx].Indices += e.NbAccessors
			}
			if mesh.Primitives[idx].Material != nil {
				*mesh.Primitives[idx].Material += e.NbMaterials
			} else {
				*mesh.Primitives[idx].Material = 0
			}
		}
		e.doc.Meshes = append(e.doc.Meshes, mesh)
	}
}

func (e *GltfMerger) mergeSkins(doc *gltf.Document) {
	for _, skin := range doc.Skins {
		if skin.InverseBindMatrices != nil {
			*skin.InverseBindMatrices += e.NbAccessors
		}
		if skin.Skeleton != nil {
			*skin.Skeleton += e.NbNodes
		}
		for idx := range skin.Joints {
			skin.Joints[idx] += e.NbNodes
		}
		e.doc.Skins = append(e.doc.Skins, skin)
	}
}
func (e *GltfMerger) mergeTextures(doc *gltf.Document) {
	for _, texture := range doc.Textures {
		if texture.Source != nil {
			*texture.Source += e.NbImages
		}
		if texture.Sampler != nil {
			*texture.Sampler += e.NbSamplers
		}
		e.doc.Textures = append(e.doc.Textures, texture)
	}
}

func (e *GltfMerger) mergeImages(doc *gltf.Document) {
	for _, image := range doc.Images {
		if image.BufferView != nil {
			*image.BufferView += e.NbBufferViews
		}
		e.doc.Images = append(e.doc.Images, image)
	}
}
func (e *GltfMerger) mergeSamplers(doc *gltf.Document) {
	for _, sampler := range doc.Samplers {
		e.doc.Samplers = append(e.doc.Samplers, sampler)
	}
}
func (e *GltfMerger) mergeMaterials(doc *gltf.Document) {
	for _, material := range doc.Materials {
		if material.PBRMetallicRoughness != nil {
			if material.PBRMetallicRoughness.BaseColorTexture != nil {
				material.PBRMetallicRoughness.BaseColorTexture.Index += e.NbTextures
				material.PBRMetallicRoughness.BaseColorTexture.TexCoord += e.NbTextures
			}
			if material.PBRMetallicRoughness.MetallicRoughnessTexture != nil {
				material.PBRMetallicRoughness.MetallicRoughnessTexture.Index += e.NbTextures
				material.PBRMetallicRoughness.MetallicRoughnessTexture.TexCoord += e.NbTextures

			}
		}
		if material.NormalTexture != nil {
			if material.NormalTexture.Index != nil {
				*material.NormalTexture.Index += e.NbTextures
				material.NormalTexture.TexCoord += e.NbTextures
			}
		}
		if material.OcclusionTexture != nil {
			if material.OcclusionTexture.Index != nil {
				*material.OcclusionTexture.Index += e.NbTextures
				material.OcclusionTexture.TexCoord += e.NbTextures
			}
		}
		if material.EmissiveTexture != nil {
			material.EmissiveTexture.Index += e.NbTextures
			material.EmissiveTexture.TexCoord += e.NbTextures

		}
		e.doc.Materials = append(e.doc.Materials, material)
	}
}

func (e *GltfMerger) mergeCameras(doc *gltf.Document) {
	for _, camera := range doc.Cameras {
		e.doc.Cameras = append(e.doc.Cameras, camera)
	}
}

func (e *GltfMerger) mergeAnimations(doc *gltf.Document) {
	for _, animation := range doc.Animations {
		for idx := range animation.Channels {
			animation.Channels[idx].Sampler += e.NbSamplers
			*animation.Channels[idx].Target.Node += e.NbNodes
		}
		for idx := range animation.Samplers {
			animation.Samplers[idx].Input += e.NbAccessors
			animation.Samplers[idx].Output += e.NbAccessors
		}
		e.doc.Animations = append(e.doc.Animations, animation)
	}
}
