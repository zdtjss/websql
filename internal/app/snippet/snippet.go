package snippet

import (
	"websql/internal/pkg/appctx"
	"websql/internal/pkg/jsonutil"
	"websql/internal/pkg/response"

	"github.com/gin-gonic/gin"
)

// List GET /api/snippet/list?keyword=&category=&tag=
// 返回当前用户的收藏列表，支持按关键字、分类、标签过滤。
func List(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	keyword := c.Query("keyword")
	category := c.Query("category")
	tag := c.Query("tag")
	// category=all 视为不过滤
	if category == "all" {
		category = ""
	}
	list, err := getDefaultSnippet().List(userId, keyword, category, tag)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, gin.H{
		"items": list,
		"total": len(list),
	})
}

// Save POST /api/snippet/save
// 新增或更新收藏，id 为空表示新增。
func Save(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	req := &SnippetSave{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	sn, err := getDefaultSnippet().Save(req, userId)
	if err != nil {
		switch err {
		case ErrTitleRequired, ErrSqlRequired:
			response.WriteErr(c, 200, 400, err.Error())
		case ErrSnippetNotFound:
			response.WriteErr(c, 200, 400, err.Error())
		default:
			response.WriteErr(c, 200, 500, "操作失败")
		}
		return
	}
	response.WriteOK(c, sn)
}

// Delete POST /api/snippet/delete?id=xxx
// 删除收藏，仅创建者可删。使用 POST 遵循 CSRF 防护规范。
func Delete(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	id := c.Query("id")
	if id == "" {
		response.WriteErr(c, 200, 400, "缺少 id 参数")
		return
	}
	if err := getDefaultSnippet().Delete(id, userId); err != nil {
		response.WriteErr(c, 200, 400, err.Error())
		return
	}
	response.WriteOK(c, "删除成功")
}

// Export GET /api/snippet/export
// 导出当前用户全部收藏为 JSON。
func Export(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	data, err := getDefaultSnippet().Export(userId)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, data)
}

// Import POST /api/snippet/import
// 导入 JSON，body 结构为 { "items": [...] }。
func Import(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	req := &SnippetImportReq{}
	if err := jsonutil.UnmarshalJson(c.Request.Body, req); err != nil {
		response.WriteErr(c, 200, 400, "请求参数解析失败")
		return
	}
	count, err := getDefaultSnippet().Import(req.Items, userId)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, gin.H{"count": count})
}

// Categories GET /api/snippet/categories
// 返回当前用户的分类统计，供前端分类树展示。
func Categories(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	cats, err := getDefaultSnippet().Categories(userId)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, cats)
}

// Tags GET /api/snippet/tags
// 返回当前用户全部标签，供前端标签过滤。
func Tags(c *gin.Context) {
	userId := appctx.Ctx.GetUserID(c)
	if userId == "" {
		response.WriteAuthErr(c, "")
		return
	}
	tags, err := getDefaultSnippet().AllTags(userId)
	if err != nil {
		response.WriteErr(c, 200, 500, "操作失败")
		return
	}
	response.WriteOK(c, tags)
}
