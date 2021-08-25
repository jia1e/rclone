/*
 * OAuth 2.0
 *
 * No description provided (generated by Swagger Codegen https://github.com/swagger-api/swagger-codegen)
 *
 * API version: 1.0.0
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */
package oauth2

type CreateClientRequest struct {
	// 一个URI字符串数组，在基于重定向的OAuth许可类型中使用，比如authorization_ code和implicit
	RedirectUris []string `json:"redirect_uris"`
	// * 客户端获取令牌所使用的许可类型。该字段使用的值与令牌端点上grant_type 参数使用的值相同。   * 授权码许可（authorization_code），客户端将资源拥有者引导至授权端点，获取授权码，然后将授权码发回至令牌端点。需要对应使用的response_type为\"code\" * 隐式许可（implicit），客户端将资源拥有者引导至授权端点，直接获取令牌。需要对应使用的response_type为\"token id_token\" * 刷新令牌许可（refresh_token），在资源拥有者不在场的情况下，客户端使用刷新令牌获取新的访问令牌 * 注册时需要注册所有字段确保客户端功能完整
	GrantTypes []string `json:"grant_types"`
	// 客户端使用的授权端点响应类型。客户端注册时需要注册全部字段确保客户端功能完整。
	ResponseTypes []string `json:"response_types"`
	// 可读的客户端显示名称
	ClientName string `json:"client_name"`
	// 客户端请求令牌时所有可用的权限范围。它的值是以空格分隔的字符串，与 OAuth协议中的同名字段一样。权限范围字段参考Authentication章节。注册时应包含\"offline\",\"openid\"和\"all\"。
	Scope string `json:"scope"`
	// 执行注销时，发送注销用户请求需要参数post_logout_redirect_uri与其他参数（参考\"注销用户\"请求）,客户端发送注销用户请求可以请求使用post_logout_redirect_uri参数将最终用户的用户代理（浏览器）重定向到的客户端注册时提供的post_logout_reditect_uris的URL数组中的一个url。post_logout_reditect_uris注册的值必须与至少一个已注册的redirect_uri的方案，域，端口匹配。
	PostLogoutRedirectUris []string  `json:"post_logout_redirect_uris"`
	Metadata               *ModelMap `json:"metadata"`
}

type ModelMap struct {
	Device *Device `json:"device"`
}
