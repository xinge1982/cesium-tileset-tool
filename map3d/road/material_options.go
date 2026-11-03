package road

import (
	"fmt"
	"strings"
)

// Select a texture or a solid color for one named model part.
// An empty entry inherits the next available material in the fallback chain.
type RoadMaterialOptions struct {
	TexturePath string
	Color       string
}

// Reject misspelled parts/subtypes and ambiguous texture/color entries before generation.
func ValidateRoadMaterials(buildType BuildType, materials map[string]RoadMaterialOptions, overrides map[string]map[string]RoadMaterialOptions) error {
	var parts, subtypes map[string]bool
	switch buildType {
	case BuildTypeRoadSurface:
		parts = map[string]bool{"top": true, "side": true, "bottom": true}
		subtypes = map[string]bool{string(BuildTypeRoadSurface): true}
	case BuildTypeBarrierWave:
		parts = map[string]bool{"rail": true, "post": true}
		subtypes = map[string]bool{"wave_two": true, "wave_three": true, "wave_nose_end": true}
	default:
		return fmt.Errorf("unsupported road material build type %q", buildType)
	}
	validate := func(entries map[string]RoadMaterialOptions, location string) error {
		for part, material := range entries {
			if !parts[part] {
				return fmt.Errorf("%s: unsupported %s material part %q", location, buildType, part)
			}
			if strings.TrimSpace(material.TexturePath) != "" && strings.TrimSpace(material.Color) != "" {
				return fmt.Errorf("%s.%s: choose texturePath or color, not both", location, part)
			}
		}
		return nil
	}
	if err := validate(materials, "materials"); err != nil {
		return err
	}
	for subtype, entries := range overrides {
		if !subtypes[subtype] {
			return fmt.Errorf("materialOverrides: unsupported %s subtype %q", buildType, subtype)
		}
		if err := validate(entries, "materialOverrides."+subtype); err != nil {
			return err
		}
	}
	return nil
}

// Resolve subtype part overrides, source defaults, legacy textures and built-in colors.
// Business parts map to the existing material slots used by each geometry builder.
func ResolveRoadMaterials(buildType BuildType, subtype, texturePath string, materials map[string]RoadMaterialOptions, overrides map[string]map[string]RoadMaterialOptions) (MaterialSet, error) {
	if err := ValidateRoadMaterials(buildType, materials, overrides); err != nil {
		return MaterialSet{}, err
	}
	texturePath = strings.TrimSpace(texturePath)
	color := func(value string) MaterialRef {
		return MaterialRef{Color: value, Mode: MaterialModeColor, DoubleSided: true}
	}
	legacy := func(fallback MaterialRef) MaterialRef {
		if texturePath != "" {
			return MaterialRef{Path: texturePath, Mode: MaterialModeTexture, DoubleSided: true}
		}
		return fallback
	}
	choose := func(part string, fallback MaterialRef) MaterialRef {
		for _, entries := range []map[string]RoadMaterialOptions{overrides[subtype], materials} {
			entry := entries[part]
			if path := strings.TrimSpace(entry.TexturePath); path != "" {
				return MaterialRef{Path: path, Mode: MaterialModeTexture, DoubleSided: true}
			}
			if value := strings.TrimSpace(entry.Color); value != "" {
				return color(value)
			}
		}
		return fallback
	}
	switch buildType {
	case BuildTypeRoadSurface:
		if subtype != string(BuildTypeRoadSurface) {
			return MaterialSet{}, fmt.Errorf("unsupported road surface material subtype %q", subtype)
		}
		fallback := legacy(color("#555555"))
		return MaterialSet{Top: choose("top", fallback), Side: choose("side", fallback), Bottom: choose("bottom", fallback)}, nil
	case BuildTypeBarrierWave:
		if subtype != "wave_two" && subtype != "wave_three" && subtype != "wave_nose_end" {
			return MaterialSet{}, fmt.Errorf("unsupported wave material subtype %q", subtype)
		}
		// Historically TexturePath applies only to the board; keep unconfigured posts gray.
		return MaterialSet{Top: choose("rail", legacy(color("#B8BDC5"))), Side: choose("post", color("#8D949C"))}, nil
	}
	return MaterialSet{}, fmt.Errorf("unsupported road material build type %q", buildType)
}
