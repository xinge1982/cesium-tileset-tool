package traffic_feature

import (
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/utils"
	"fmt"
	"reflect"
	"strings"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type SignEdit struct {
	ID          int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SignTable   string         `gorm:"column:sign_table" json:"signTable"`
	TID         string         `gorm:"column:tid;" json:"tid"`
	ClassName   string         `gorm:"column:class_name;" json:"className"`
	Direction   string         `gorm:"column:direction;" json:"direction"`
	GroupID     string         `gorm:"column:group_id" json:"groupId"`
	RoadId      int64          `gorm:"column:road_id;comment:'道路编号'" json:"roadId"`
	ObjAngle    float64        `gorm:"column:obj_angle" json:"objAngle"`
	Pic         string         `gorm:"column:pic" json:"pic"`
	Type        string         `gorm:"column:type;default:'';comment:'类型'" json:"type"`
	SType       string         `gorm:"column:s_type;default:'';comment:'子类型'" json:"sType"`
	GBType      string         `gorm:"column:gb_type;default:'';comment:'国标类型'" json:"gbType"`
	Shape       string         `gorm:"column:shape;comment:'形状'" json:"shape"`
	Width       float64        `gorm:"column:width;comment:'宽度'" json:"width"`
	Height      float64        `gorm:"column:height;comment:'高度'" json:"height"`
	Depth       float64        `gorm:"column:depth;comment:'厚度'" json:"depth"`
	Radius      float64        `gorm:"column:radius;comment:'半径'" json:"radius"`
	Model       string         `gorm:"column:model;comment:'模型'" json:"model"`
	Geom        string         `gorm:"column:geom;type:geometry(PointZ,4326);comment:'坐标'" json:"geom"`
	PhotoQuery  string         `gorm:"column:photo_query;size:255;comment:照片参数;default:''" json:"photoQuery"`
	KmPileName  string         `gorm:"km_pile_name;comment:公里桩名称" json:"kmPileName"`
	Remark      string         `gorm:"column:remark;size:255;comment:'备注'" json:"remark"`
	ModelData   datatypes.JSON `gorm:"column:model_data;type:jsonb;comment:附加数据" json:"modelData"` // 存储模型的modeler的content数据
	ServiceData datatypes.JSON `gorm:"column:service_data;type:jsonb;comment:业务数据" json:"serviceData"`
	EditStatus  string         `gorm:"column:edit_status;size:128;default:'draft';comment:'编辑状态: draft/submitted/approved/rejected'" json:"editStatus"`
	DelFlag     string         `gorm:"column:del_flag;comment:'删除状态 1 删除 0 正常'" json:"delFlag"` // 删除标志（0代表存在 2代表删除）
	NeedUpdate  int            `gorm:"column:need_update;comment:'是否需要保存到正式的标牌表 1 是 0 否'" json:"needUpdate"`
	UpdateTime  pg.JSONTime    `gorm:"column:update_time;comment:'状态更新时间'" json:"updateTime"`
	UpdateBy    string         `gorm:"column:update_by" json:"updateBy"` // 更新者
	ImageBase64 string         `gorm:"-" json:"imageBase64"`
}

// TableName 设置表名
func (SignEdit) TableName() string {
	return "t_base_sign_edit"
}

type GantryEdit struct {
	ID          int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	GantryTable string         `gorm:"column:gantry_table;comment:'数据表名称'" json:"gantry_table"`
	GantryId    string         `gorm:"column:gantry_id;comment:'编号'" json:"gantryId"`
	GroupId     string         `gorm:"column:group_id;comment:'编组'" json:"groupId"`
	RoadId      int64          `gorm:"column:road_id;comment:'道路编号'" json:"roadId"`
	ObjAngle    float64        `gorm:"column:obj_angle;comment:'角度'" json:"objAngle"`
	ScaleX      float64        `gorm:"column:scale_x;comment:'左右缩放';default:1" json:"scaleX"`
	ScaleY      float64        `gorm:"column:scale_y;comment:'前后缩放';default:1" json:"scaleY"`
	ScaleZ      float64        `gorm:"column:scale_z;comment:'上下缩放';default:1" json:"scaleZ"`
	Type        string         `gorm:"column:type;default:'';comment:'类型'" json:"type"`
	SType       string         `gorm:"column:s_type;default:'';comment:'子类型'" json:"sType"`
	Width       float64        `gorm:"column:width" json:"width"`
	Length      float64        `gorm:"column:length" json:"length"`
	Height      float64        `gorm:"column:height" json:"height"`
	Model       string         `gorm:"column:model;comment:'模型'" json:"model"`
	Geom        string         `gorm:"column:geom;type:geometry(PointZ,4326);comment:'坐标'" json:"geom"`
	PhotoQuery  string         `gorm:"column:photo_query;size:255;comment:照片参数;default:''" json:"photoQuery"`
	Remark      string         `gorm:"column:remark;size:255;comment:'备注'" json:"remark"`
	ModelData   datatypes.JSON `gorm:"column:model_data;type:jsonb;comment:附加数据" json:"modelData"` // 存储模型的modeler的content数据
	ServiceData datatypes.JSON `gorm:"column:service_data;type:jsonb;comment:业务数据" json:"serviceData"`
	EditStatus  string         `gorm:"column:edit_status;size:128;default:'draft';comment:'编辑状态: draft/submitted/approved/rejected'" json:"editStatus"`
	DelFlag     string         `gorm:"column:del_flag;comment:'删除状态 1 删除 0 正常'" json:"delFlag"` // 删除标志（0代表存在 2代表删除）
	NeedUpdate  int            `gorm:"column:need_update;comment:'是否需要保存到正式的门架表 1 是 0 否'" json:"needUpdate"`
	UpdateTime  pg.JSONTime    `gorm:"column:update_time;comment:'状态更新时间'" json:"updateTime"`
	UpdateBy    string         `gorm:"column:update_by" json:"updateBy"` // 更新者
}

// TableName 设置表名
func (GantryEdit) TableName() string {
	return "t_base_gantry_edit"
}

type PoleEdit struct {
	ID          int64          `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PoleTable   string         `gorm:"column:pole_table" json:"pole_table"`
	PoleId      string         `gorm:"column:pole_id" json:"poleId"`
	GroupId     string         `gorm:"column:group_id" json:"groupId"`
	RoadId      int64          `gorm:"column:road_id;comment:'道路编号'" json:"roadId"`
	ObjAngle    float64        `gorm:"column:obj_angle" json:"objAngle"`
	Type        string         `gorm:"column:type;default:'';comment:'类型'" json:"type"`
	SType       string         `gorm:"column:s_type;default:'';comment:'子类型'" json:"sType"`
	Model       string         `gorm:"column:model;comment:'模型'" json:"model"`
	Geom        string         `gorm:"column:geom;type:geometry(PointZ,4326);comment:'坐标'" json:"geom"`
	PhotoQuery  string         `gorm:"column:photo_query;size:255;comment:照片参数;default:''" json:"photoQuery"`
	Remark      string         `gorm:"column:remark;size:255;comment:'备注'" json:"remark"`
	Metadata    datatypes.JSON `gorm:"column:metadata;type:jsonb;comment:附加数据" json:"metadata"`    // 存储用户数据
	ModelData   datatypes.JSON `gorm:"column:model_data;type:jsonb;comment:附加数据" json:"modelData"` // 存储模型的modeler的content数据
	ServiceData datatypes.JSON `gorm:"column:service_data;type:jsonb;comment:业务数据" json:"serviceData"`
	EditStatus  string         `gorm:"column:edit_status;size:128;default:'draft';comment:'编辑状态: draft/submitted/approved/rejected'" json:"editStatus"`
	DelFlag     string         `gorm:"column:del_flag;comment:'删除状态 1 删除 0 正常'" json:"delFlag"` // 删除标志（0代表存在 2代表删除）
	NeedUpdate  int            `gorm:"column:need_update;comment:'是否需要保存到正式的标志杆表 1 是 0 否'" json:"needUpdate"`
	UpdateTime  pg.JSONTime    `gorm:"column:update_time;comment:'状态更新时间'" json:"updateTime"`
	UpdateBy    string         `gorm:"column:update_by" json:"updateBy"` // 更新者
}

// TableName 设置表名
func (PoleEdit) TableName() string {
	return "t_base_pole_edit"
}

type DeviceEdit struct {
	ID             int64           `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	TID            string          `gorm:"column:tid;" json:"tid"`
	ClassName      string          `gorm:"column:class_name;" json:"className"`
	Direction      string          `gorm:"column:direction;" json:"direction"`
	Geom           string          `gorm:"column:geom;type:geometry(PointZ,4326)" json:"geom"`
	Fid            int             `gorm:"column:fid" json:"fid"`
	GroupID        string          `gorm:"column:group_id" json:"groupId"`
	PPole          float64         `gorm:"column:p_pole" json:"pPole"`
	MinDist        float64         `gorm:"column:min_dist" json:"minDist"`
	Angle          float64         `gorm:"column:angle" json:"angle"`
	Angle2         float64         `gorm:"column:angle2" json:"angle2"`
	PosType        float64         `gorm:"column:pos_type" json:"posType"`
	HeightType     float64         `gorm:"column:height_type" json:"heightType"`
	SideType       float64         `gorm:"column:side_type" json:"sideType"`
	Md5            string          `gorm:"column:md5" json:"md5"`
	InterID        string          `gorm:"column:inter_id" json:"interId"`
	Modified       float64         `gorm:"column:modified" json:"modified"`
	ZValue         float64         `gorm:"column:z_value" json:"zValue"`
	Mesh           string          `gorm:"column:mesh" json:"mesh"`
	RoadID         int64           `gorm:"column:road_id" json:"roadId"`
	Type           string          `gorm:"column:type" json:"type"`
	SType          string          `gorm:"column:s_type" json:"sType"`
	Cjzp           string          `gorm:"column:cjzp" json:"cjzp"`
	PileNum        string          `gorm:"column:pile_num" json:"pileNum"`
	Sbcx           string          `gorm:"column:sbcx" json:"sbcx"`
	Bz             string          `gorm:"column:bz" json:"bz"`
	TrafficEneID   int64           `gorm:"column:traffic_ene_id" json:"trafficEneId"`
	PoleID         int64           `gorm:"column:pole_id" json:"poleId"`
	GdName         string          `gorm:"column:gd_name" json:"gdName"`
	GdRoadNo       string          `gorm:"column:gd_road_no" json:"gdRoadNo"`
	GdRampBs       string          `gorm:"column:gd_ramp_bs" json:"gdRampBs"`
	GdTravelType   string          `gorm:"column:gd_traveltype" json:"gdTravelType"`
	GdRampNo       string          `gorm:"column:gd_ramp_no" json:"gdRampNo"`
	GdMilleage     string          `gorm:"column:gd_milleage" json:"gdMilleage"`
	GdLaneInfo     string          `gorm:"column:gd_lane_info" json:"gdLaneInfo"`
	GandongCode    string          `gorm:"column:gandong_code" json:"gandongCode"`
	PoleType       int             `gorm:"column:pole_type" json:"poleType"`
	ParentID       int             `gorm:"column:parent_id" json:"parentId"`
	ParentType     string          `gorm:"column:parent_type" json:"parentType"`
	Model          string          `gorm:"column:model" json:"model"`
	Dtype          int             `gorm:"column:dtype" json:"dtype"`
	Ftype          int             `gorm:"column:ftype" json:"ftype"`
	Ctype          int             `gorm:"column:ctype" json:"ctype"`
	ObjAngle       float64         `gorm:"column:obj_angle" json:"objAngle"`
	Length         float64         `gorm:"column:length" json:"length"`
	Height         float64         `gorm:"column:height" json:"height"`
	Width          float64         `gorm:"column:width" json:"width"`
	Classification int             `gorm:"column:classification" json:"classification"`
	Colour         string          `gorm:"column:colour" json:"colour"`
	PitchAngle     float64         `gorm:"column:pitch_angle" json:"pitchAngle"`
	XxbID          int             `gorm:"column:xxb_id" json:"xxbId"`
	ChnName        string          `gorm:"column:chn_name" json:"chnName"`
	LatchOn        string          `gorm:"column:latch_on" json:"latchOn"`
	CodeID         string          `gorm:"column:codeid" json:"codeId"`
	PhotoQuery     string          `gorm:"column:photo_query;size:255;comment:照片参数;default:''" json:"photoQuery"`
	Remark         string          `gorm:"column:remark;size:255;comment:'备注'" json:"remark"`
	Transform      pq.Float64Array `gorm:"column:transform;type:double precision[]" json:"transform"` // PostgreSQL double precision[]
	Metadata       datatypes.JSON  `gorm:"column:metadata;type:jsonb;comment:附加数据" json:"metadata"`   // 存储模型transform相关和用户数据
	ModelData      datatypes.JSON  `gorm:"column:model_data;type:jsonb;comment:附加数据" json:"modelData"`
	ServiceData    datatypes.JSON  `gorm:"column:service_data;type:jsonb;comment:服务数据" json:"serviceData"`
	DeviceTable    string          `gorm:"column:device_table;size:256;comment:标牌表名" json:"deviceTable"`
	EditStatus     string          `gorm:"column:edit_status;size:128;default:'draft';comment:'编辑状态: draft/submitted/approved/rejected'" json:"editStatus"`
	DelFlag        string          `gorm:"column:del_flag;comment:'删除状态 1 删除 0 正常'" json:"delFlag"` // 删除标志（0代表存在 2代表删除）
	NeedUpdate     int             `gorm:"column:need_update;comment:'是否需要保存到正式的设备表 1 是 0 否'" json:"needUpdate"`
	UpdateTime     pg.JSONTime     `gorm:"column:update_time;comment:'状态更新时间'" json:"updateTime"`
	UpdateBy       string          `gorm:"column:update_by" json:"updateBy"` // 更新者
}

// TableName 设置表名
func (DeviceEdit) TableName() string {
	return "t_base_device_edit"
}

func buildQuery[T any](order, orderDir string, db *gorm.DB, conditions map[string]interface{}) *gorm.DB {
	var sample T
	dbQuery := db.Model(&sample)

	if conditions["updateTime"] != nil {
		dbQuery.Where("update_time >= ? ", conditions["updateTime"])
	}

	// 动态组合条件
	t := reflect.TypeOf(sample)
	for i := 0; i < t.NumField(); i++ {
		key := t.Field(i).Tag.Get("json")
		if key == "" {
			continue
		}
		if conditions[key] != nil {
			// 取数据表列名
			gormTag := t.Field(i).Tag.Get("gorm")
			if gormTag == "" {
				continue
			}
			columnName := ""
			gormKeys := strings.Split(gormTag, ";")
			for _, gormKey := range gormKeys {
				gormKey = strings.Trim(gormKey, " ")
				if strings.HasPrefix(gormKey, "column:") {
					columnName = strings.Replace(gormKey, "column:", "", 1)
				}
			}
			if columnName == "" {
				continue
			}

			prefixTag := t.Field(i).Tag.Get("prefix")
			if prefixTag != "" {
				if strings.Contains(prefixTag, ",") {
					keys := strings.Split(prefixTag, ",")
					var cols []string
					for _, s := range keys {
						cols = append(cols, strings.Trim(s+"."+columnName, " "))
					}
					columnName = "COALESCE(" + strings.Join(cols, ", ") + ")"
				} else {
					columnName = prefixTag + "." + columnName
				}
			}

			// 判断字段类型
			switch t.Field(i).Type.Kind() {
			case reflect.String:
				if utils.ToString(conditions[key]) == "" || utils.ToString(conditions[key]) == "-" {
					dbQuery.Where(columnName + " = ''")
				} else {
					if list, ok := conditions[key].([]interface{}); ok {
						var conds []string
						for _, l := range list {
							conds = append(conds, utils.ToString(l))
						}
						matchWithMultipleConditions(dbQuery, columnName, conds)
					} else if strings.Contains(utils.ToString(conditions[key]), "%") {
						dbQuery.Where(columnName + " like '" + utils.ToString(conditions[key]) + "'")
					} else {
						dbQuery.Where(columnName + " = '" + utils.ToString(conditions[key]) + "'")
					}
				}
			case reflect.Int:
				dbQuery.Where(columnName+" = ? ", utils.ToInt64(conditions[key]))
			default:
				dbQuery.Where(columnName+" = ? ", utils.ToString(conditions[key]))
			}
		}
	}

	// 动态添加排序条件
	if conditions["pageSize"] != nil && conditions["pageNum"] != nil {
		pageSize := int(utils.ToInt32(conditions["pageSize"]))
		pageNum := int(utils.ToInt32(conditions["pageNum"]))
		dbQuery = dbQuery.Order(order + " " + orderDir).Limit(pageSize).Offset((pageNum - 1) * pageSize)
	} else if len(order) > 0 && len(orderDir) > 0 {
		dbQuery = dbQuery.Order(order + " " + orderDir)
	}

	return dbQuery
}

func validateMapFields(input map[string]interface{}, structType interface{}) bool {
	// 获取结构体的类型
	t := reflect.TypeOf(structType)
	if t.Kind() != reflect.Struct {
		fmt.Println("Provided type is not a struct.")
		return false
	}

	// 递归解析结构体字段
	validFields := extractFields(structType)

	// 检查 map 中的键是否在结构体字段中存在
	for key := range input {
		if !validFields[key] {
			fmt.Printf("Invalid field: %s\n", key)
			return false
		}
	}

	return true
}

// 递归提取所有字段
func extractFields(structType interface{}) map[string]bool {
	fields := make(map[string]bool)
	t := reflect.TypeOf(structType)
	if t.Kind() == reflect.Ptr {
		t = t.Elem() // 获取指针所指向的元素
	}

	if t.Kind() != reflect.Struct {
		return fields
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 如果字段是嵌入结构体，递归提取其字段
		if field.Anonymous && field.Type.Kind() == reflect.Ptr {
			embeddedFields := extractFields(reflect.New(field.Type.Elem()).Elem().Interface())
			for k, v := range embeddedFields {
				fields[k] = v
			}
		} else if field.Anonymous && field.Type.Kind() == reflect.Struct {
			embeddedFields := extractFields(reflect.New(field.Type).Elem().Interface())
			for k, v := range embeddedFields {
				fields[k] = v
			}
		} else {
			jsonName := field.Tag.Get("json")
			if jsonName != "" {
				fields[jsonName] = true
			}
		}
	}
	return fields
}

func matchWithMultipleConditions(db *gorm.DB, field string, conditions []string) {
	// 动态构建 WHERE 子句
	var query string
	for i, condition := range conditions {
		if i > 0 {
			query += " OR "
		}
		if condition == "none" && field == "status" {
			query += " (" + field + " IS NULL OR " + field + " = 'none')"
		} else {
			query += field + " LIKE '%" + condition + "%'"
		}
	}

	// 执行查询
	db = db.Where(query)
}
