/*
 * 文档访问
 *
 * API to access AnyShare    如有任何疑问，可到开发者社区提问：https://developers.aishu.cn  # Authentication  - 调用需要鉴权的API，必须将token放在HTTP header中：\"Authorization: Bearer ACCESS_TOKEN\"  - 对于GET请求，除了将token放在HTTP header中，也可以将token放在URL query string中：\"tokenid=ACCESS_TOKEN\"  
 *
 * API version: 1.0.0
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */
package efast

type FileOsdownloadRes struct {
	// - authrequest[0]：请求方法 - authrequest[1]：资源URL - authrequest[2]（如果存在）及以后项为http请求的headers，如自定义date，Content-type等，格式为“key: value”
	Authrequest []string `json:"authrequest"`
	// 由客户端设置的文件本地修改时间 下载第0块时返回，若未设置，返回modified的值
	ClientMtime int64 `json:"client_mtime"`
	// 编辑者名称， UTF8编码
	Editor string `json:"editor"`
	// 上传时间， UTC时间，此为上传版本时的服务器时间
	Modified int64 `json:"modified"`
	// 文件的当前名称，UTF8编码
	Name string `json:"name"`
	// 文件版本号
	Rev string `json:"rev"`
	// 当前下载版本的总大小
	Size int64 `json:"size"`
}
