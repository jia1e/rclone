/*
 * 文档访问
 *
 * API to access AnyShare    如有任何疑问，可到开发者社区提问：https://developers.aishu.cn  # Authentication  - 调用需要鉴权的API，必须将token放在HTTP header中：\"Authorization: Bearer ACCESS_TOKEN\"  - 对于GET请求，除了将token放在HTTP header中，也可以将token放在URL query string中：\"tokenid=ACCESS_TOKEN\"  
 *
 * API version: 1.0.0
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */
package efast

type FileMoveRes struct {
	// 返回新的gns路径
	Docid string `json:"docid"`
	// UTF8编码，仅当ondup为2时才返回，否则返回参数仍然为空
	Name string `json:"name,omitempty"`
}
