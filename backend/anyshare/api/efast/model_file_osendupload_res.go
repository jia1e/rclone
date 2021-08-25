/*
 * 文档访问
 *
 * API to access AnyShare    如有任何疑问，可到开发者社区提问：https://developers.aishu.cn  # Authentication  - 调用需要鉴权的API，必须将token放在HTTP header中：\"Authorization: Bearer ACCESS_TOKEN\"  - 对于GET请求，除了将token放在HTTP header中，也可以将token放在URL query string中：\"tokenid=ACCESS_TOKEN\"  
 *
 * API version: 1.0.0
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */
package efast

type FileOsenduploadRes struct {
	// 编辑者
	Editor string `json:"editor"`
	// 上传时间，UTC时间，此为上传版本完成时的服务器时间
	Modified int64 `json:"modified"`
}
