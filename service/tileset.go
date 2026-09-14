package service

import (
	"cesium-tileset-tool/config"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

type TilesetMenuItem struct {
	Value             string                       `json:"value"`
	Label             string                       `json:"label"`
	Type              string                       `json:"type,omitempty"`
	Url               string                       `json:"url,omitempty"`
	Options           [][]string                   `json:"options,omitempty"`
	Children          []TilesetSourceMenuItem      `json:"children"`
	IdField           string                       `json:"idField,omitempty"`
	ZClip             config.ZClipConfig           `json:"zClip,omitempty"`
	FeatureIdField    string                       `json:"featureIdField,omitempty"`    //数据对应tileset的字段名称
	FeatureValueField string                       `json:"featureValueField,omitempty"` //数据对应tileset的字段值列名称
	BoundingVolume    *config.BoundingVolumeConfig `json:"boundingVolume,omitempty"`
}

type TilesetSourceMenuItem struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type tilesetMenuSortItem struct {
	key     string
	order   int
	tileset config.TilesetConfig
}

func (service *TilesetSourceSearchService) GetTilesets(
	ctx *gin.Context,
) {
	items := service.GetTilesetMenu()

	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": items,
	})
}

func (service *TilesetSourceSearchService) GetTilesetMenu() []TilesetMenuItem {
	sortedTilesets := make(
		[]tilesetMenuSortItem,
		0,
		len(service.tilesets),
	)

	for key, tileset := range service.tilesets {
		// 不返回已经禁用的 Tileset。
		if !tileset.Enabled {
			continue
		}

		// 数据维护菜单只需要返回可维护的 Tileset。
		if !tileset.Maintainable {
			continue
		}

		sortedTilesets = append(
			sortedTilesets,
			tilesetMenuSortItem{
				key:     key,
				order:   tileset.Order,
				tileset: tileset,
			},
		)
	}

	sort.SliceStable(
		sortedTilesets,
		func(i, j int) bool {
			if sortedTilesets[i].order != sortedTilesets[j].order {
				return sortedTilesets[i].order <
					sortedTilesets[j].order
			}

			return sortedTilesets[i].key <
				sortedTilesets[j].key
		},
	)

	result := make(
		[]TilesetMenuItem,
		0,
		len(sortedTilesets),
	)

	for _, current := range sortedTilesets {
		children := make(
			[]TilesetSourceMenuItem,
			0,
			len(current.tileset.Sources),
		)

		for _, source := range current.tileset.Sources {
			if source.ID == "" {
				continue
			}

			label := source.Name
			if label == "" {
				label = source.ID
			}

			children = append(
				children,
				TilesetSourceMenuItem{
					Value: source.ID,
					Label: label,
				},
			)
		}

		// 没有配置 source 的 Tileset 无法用于数据维护。
		if len(children) == 0 {
			continue
		}

		label := current.tileset.Name
		if label == "" {
			label = current.key
		}

		featureIdField := current.tileset.Feature.IDField
		if len(current.tileset.Feature.FeatureIdField) > 0 {
			featureIdField = current.tileset.Feature.FeatureIdField
		}
		if featureIdField == "" {
			featureIdField = "id"
		}

		result = append(
			result,
			TilesetMenuItem{
				Value:          current.key,
				Url:            current.tileset.URL,
				Options:        current.tileset.Options,
				BoundingVolume: current.tileset.BoundingVolume,
				Label:          label,
				Type:           current.tileset.Type,
				Children:       children,
				IdField:        current.tileset.Feature.IDField,
				ZClip:          current.tileset.ZClip,
				FeatureIdField: featureIdField,
			},
		)
	}

	return result
}
