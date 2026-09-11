# cesium-tileset-tool

## Tileset LOD models

`tileset-lod-tool feature-tileset-build` can build a four-level LOD chain for
each Geohash leaf tile. LOD0, LOD1 and LOD2 source models are read from local
directories below `NetworkFolder`; LOD3 source models continue to be read from
MinIO.

```yaml
LOD:
  Enabled: true
  LOD0:
    ModelFolder: lod0
    GeometricError: 200
  LOD1:
    ModelFolder: lod1
    GeometricError: 80
  LOD2:
    ModelFolder: lod2
    GeometricError: 25
  LOD3:
    GeometricError: 0
```

If `NetworkFolder` is `./networks/project`, the tool creates and searches these
directories:

```text
./networks/project/lod0
./networks/project/lod1
./networks/project/lod2
```

Each directory uses the value of the database `model` field as its relative
model path. Missing local models are skipped only for that LOD. Empty LODs are
omitted from the tile node chain.

Internally, model instances are grouped by `TableName|Model` so equal model
names from different source tables cannot be merged accidentally. This group
key is never used as a file path; local and MinIO model lookup always uses the
original `model` field.

Generated tile models use a Geohash directory and one file per LOD:

```text
tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod0.glb
tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod1.glb
tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod2.glb
tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod3.glb
```

LOD nodes use `REPLACE`. Their `geometricError` values must decrease strictly,
and LOD3 must use `0`.
