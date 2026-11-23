package pluginreadme

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// 内置 cfg 英文注释翻译词典。
//
// 词典随二进制发布，插件上传时就地生成中文说明，不依赖宿主机上的外部脚本。
// 新增条目直接改 cfg-translations.json 后重新编译即可。
//
//go:embed cfg-translations.json
var translationsJSON []byte

var (
	loadOnce     sync.Once
	translations map[string]string
	loadErr      error
)

// Translations 返回内置的 cfg 英文注释翻译词典。
//
// 首次调用时解析内嵌 JSON，之后复用。解析失败返回空表而不是报错：
// 词典缺失只影响注释是否中文，不应该阻断插件说明生成。
func Translations() map[string]string {
	loadOnce.Do(func() {
		var parsed map[string]string
		if err := json.Unmarshal(translationsJSON, &parsed); err != nil {
			loadErr = err
			translations = map[string]string{}
			return
		}
		translations = parsed
	})
	return translations
}

// TranslationError 返回词典解析错误（仅用于诊断，正常流程不依赖）。
func TranslationError() error {
	Translations()
	return loadErr
}
