package main

import (
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/minioconn"
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/traffic_feature"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	log "github.com/sirupsen/logrus"
)

func NewFeatureTileSetBuildCommand() *FeatureTileSetBuildCommand {
	gc := &FeatureTileSetBuildCommand{
		fs: flag.NewFlagSet("feature-tileset-build", flag.ContinueOnError),
	}

	gc.fs.StringVar(&gc.path, "c", "config.yaml", "path of config file")
	gc.fs.StringVar(&gc.outputPath, "output-path", "tilesets", "output tileset path")
	gc.fs.StringVar(&gc.tilesetType, "tileset-type", "features", "output tileset type")
	gc.fs.BoolVar(&gc.removeModel, "remove-model", false, "remove model")

	return gc
}

type FeatureTileSetBuildCommand struct {
	fs *flag.FlagSet

	path        string
	outputPath  string
	removeModel bool
	tilesetType string
}

func (g *FeatureTileSetBuildCommand) Name() string {
	return g.fs.Name()
}

func (g *FeatureTileSetBuildCommand) Init(args []string) error {
	return g.fs.Parse(args)
}

func (g *FeatureTileSetBuildCommand) Run() error {
	fmt.Println("Command feature-tileset-build: ", g.path)
	// config file
	if _, err := os.Stat(g.path); err == nil {
		config.ConfigFilePath = g.path
	} else {
		log.Fatal(fmt.Errorf("error in config file, file not exists: %s", g.path))
	}

	var cfg = config.Instance()
	cfg.AppVersion = Version
	cfg.BuildDate = BuildDate
	cfg.GitCommit = GitCommit
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	configName := ""
	_, fn := filepath.Split(g.path)
	ext := filepath.Ext(fn)
	configName = strings.ReplaceAll(fn, ext, "")

	conn := pg.GetConn(configName)
	if conn == nil {
		log.Fatal("Error initial DB connection of project " + configName)
		return nil
	}
	retries := 0
	maxRetries := 10
	for {
		if !conn.Ready {
			time.Sleep(3 * time.Second)
			if retries += 1; retries > maxRetries {
				log.Fatal("Error Project DB connect failed")
			}
		} else {
			break
		}
	}
	log.Infof("begin build tileset to %s of type %s in project %s", g.outputPath, g.tilesetType, configName)

	if g.removeModel {
		client := minioconn.GetMinioConn(configName)
		if client == nil {
			log.Fatal("minio client not exists")
		}
		{
			polePrefix := ""
			poleBucket := cfg.MinioOutput.Bucket
			if cfg.MinioOutput.PoleGltfBucket != "" {
				var keys = strings.Split(cfg.MinioOutput.PoleGltfBucket, "/")
				if len(keys) > 0 {
					poleBucket = keys[0]
					if len(keys) > 1 {
						polePrefix = strings.Join(keys[1:], "/")
					}
				}
			}
			if len(polePrefix) == 0 {
				log.Fatal(fmt.Errorf("error in config file, prefix empty: %s", cfg.MinioOutput.PoleGltfBucket))
			}

			ctx := context.Background()
			// List objects
			errD := client.RemoveAllObjects(ctx, poleBucket, minio.ListObjectsOptions{
				Prefix:    polePrefix,
				Recursive: true,
			})
			if errD != nil {
				log.Fatal(errD)
			}

			fmt.Println(fmt.Sprintf("All objects deleted under bucket %s prefix %s", poleBucket, polePrefix))
		}
		{
			signPrefix := ""
			signBucket := cfg.MinioOutput.Bucket
			if cfg.MinioOutput.SignGltfBucket != "" {
				var keys = strings.Split(cfg.MinioOutput.SignGltfBucket, "/")
				if len(keys) > 0 {
					signBucket = keys[0]
					if len(keys) > 1 {
						signPrefix = strings.Join(keys[1:], "/")
					}
				}
			}
			if len(signPrefix) == 0 {
				log.Fatal(fmt.Errorf("error in config file, prefix empty: %s", cfg.MinioOutput.SignGltfBucket))
			}

			ctx := context.Background()
			// List objects
			errD := client.RemoveAllObjects(ctx, signBucket, minio.ListObjectsOptions{
				Prefix:    signPrefix,
				Recursive: true,
			})
			if errD != nil {
				log.Fatal(errD)
			}

			fmt.Println(fmt.Sprintf("All objects deleted under bucket %s prefix %s", signBucket, signPrefix))
		}
	}

	var db = conn.DB
	var geoTable = traffic_feature.GeoTable{
		Threshold:      50,
		TilesetSources: make(map[string]config.TilesetSourceConfig),
	}
	for _, tc := range config.Instance().Tilesets {
		if tc.Type == g.tilesetType {
			geoTable.PartitionTableName = tc.Partition.Table
			geoTable.LOD = tc.LOD
			for _, source := range tc.Sources {
				geoTable.GeoTableNames = append(geoTable.GeoTableNames, source.Table.Name)
				geoTable.TilesetSources[source.Table.Name] = source
			}
		}
	}

	if len(geoTable.GeoTableNames) == 0 {
		log.Fatal(fmt.Errorf("error in config file, geoTable empty"))
	}

	traffic_feature.AllTiles = []traffic_feature.GeoTable{
		geoTable,
	}

	if err := traffic_feature.InitTileSetTables(db, cfg.Bound); err != nil {
		log.Fatal(err)
	}

	// 初始化 tileset 分片数据
	if errR := traffic_feature.RefineUntilStable(db, cfg.Bound); errR != nil {
		return errR
	}

	_, err := traffic_feature.GenerateAllGeoHashTile(
		configName, geoTable.PartitionTableName, g.outputPath, cfg.Bound)
	if err != nil {
		log.Fatal(err)
	}
	err = traffic_feature.RemoveExpired(
		db, time.Now().Add(-1*time.Hour), cfg.Bound)
	if err != nil {
		log.Fatal(err)
	}
	log.Infof("✅ Command feature-tileset-build success of table %s in project %s ", geoTable.PartitionTableName, configName)

	return nil
}
