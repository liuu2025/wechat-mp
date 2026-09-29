package handler

import (
	"encoding/json"
	"net/http"
	"os"

	"wechat-mp/internal/model"
	"wechat-mp/internal/storage"
)

type MenuHandler struct {
	storage *storage.Storage
}

func NewMenuHandler(store *storage.Storage) *MenuHandler {
	return &MenuHandler{storage: store}
}

// GetMenu 获取后端正式固化的菜单配置（安全只读）
func (h *MenuHandler) GetMenu(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, 405, "Method Not Allowed")
		return
	}

	menu := loadOfficialMenuFromFile()
	writeSuccess(w, menu)
}

// loadOfficialMenuFromFile 从 config/menu.json 加载权威菜单配置
func loadOfficialMenuFromFile() *model.MenuConfig {
	paths := []string{"config/menu.json", "../config/menu.json", "/app/config/menu.json"}
	for _, p := range paths {
		if data, err := os.ReadFile(p); err == nil {
			var menu model.MenuConfig
			if err := json.Unmarshal(data, &menu); err == nil && len(menu.Button) > 0 {
				return &menu
			}
		}
	}
	return defaultFallbackMenu()
}

func defaultFallbackMenu() *model.MenuConfig {
	return &model.MenuConfig{
		Button: []*model.MenuButton{
			{
				Type: "click",
				Name: "获取A验证码",
				Key:  "GET_CODE_PROJECT_A",
			},
			{
				Name: "更多项目",
				SubButton: []*model.MenuButton{
					{
						Type: "click",
						Name: "获取B验证码",
						Key:  "GET_CODE_PROJECT_B",
					},
					{
						Type: "click",
						Name: "获取C验证码",
						Key:  "GET_CODE_PROJECT_C",
					},
				},
			},
			{
				Name: "服务支持",
				SubButton: []*model.MenuButton{
					{
						Type: "click",
						Name: "使用帮助",
						Key:  "HELP_INFO",
					},
					{
						Type: "click",
						Name: "站长推荐",
						Key:  "AD_INFO",
					},
				},
			},
		},
	}
}
