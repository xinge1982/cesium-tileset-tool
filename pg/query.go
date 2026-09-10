package pg

type SysBaseQuery struct {
	BeginTime  string `json:"beginTime"`
	EndTime    string `json:"endTime"`
	ModifyTime string `json:"modifyTime"`
	PageSize   int    `json:"pageSize"`
	PageNum    int    `json:"pageNum"`
}
