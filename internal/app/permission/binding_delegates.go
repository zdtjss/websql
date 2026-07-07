package permission

import (
	"errors"
	"log"

	"websql/internal/app/conn"
)

// SaveTreeByService 保存目录树配置。
// 业务来自 SaveTree handler: 解析 tree JSON,调用 doTreeInsert 全量覆盖。
// 桌面模式默认 IsRemote=false,无需 admin 权限校验。
func SaveTreeByService(tree []*DirTree) error {
	if tree == nil {
		return errors.New("请求参数解析失败")
	}
	doTreeInsert(tree)
	return nil
}

// DelTreeNodeByService 删除目录树节点。
// 业务来自 DelTreeNode handler。
func DelTreeNodeByService(id string) error {
	if id == "" {
		return errors.New("缺少 id 参数")
	}
	getDB().Exec("delete from t_tree where id = ?", id)
	return nil
}

// ListDirTreeByService 列出所有目录树(不含连接)。
// 业务来自 ListDirTree handler: 查询 t_tree 表,构建两层树结构。
func ListDirTreeByService() []*conn.Tree {
	treeList := []*DirTree{}
	err := getDB().Select(&treeList, "select * from t_tree")
	if err != nil {
		log.Printf("[ListDirTreeByService] 查询目录树失败: %v", err)
		return nil
	}
	tree := []*conn.Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &conn.Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: conn.TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*conn.Tree{}
	for _, cfg := range tree {
		if cfg.Parent == "" {
			firstLevel = append(firstLevel, cfg)
		}
	}
	for _, cfg := range firstLevel {
		cfg.Children = findChild(cfg, tree, map[string][]*conn.ConnCfgBase{})
	}
	return firstLevel
}

// ConnBaseTreeByService 列出目录树+连接构成的完整树结构。
// 业务来自 ConnBaseTree handler: 查询 t_tree + t_conn,合并成树。
func ConnBaseTreeByService() []*conn.Tree {
	treeList := []*DirTree{}
	err := getDB().Select(&treeList, "select * from t_tree")
	if err != nil {
		log.Printf("[ConnBaseTreeByService] 查询目录树失败: %v", err)
		return nil
	}
	tree := []*conn.Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &conn.Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: conn.TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*conn.Tree{}
	for _, cfg := range tree {
		if cfg.Parent == "" {
			firstLevel = append(firstLevel, cfg)
		}
	}
	connMap := listConnBase()
	for _, cfg := range firstLevel {
		cfg.Children = append(cfg.Children, findChild(cfg, tree, connMap)...)
	}
	firstLevelConns := []*conn.ConnCfgBase{}
	err = getDB().Select(&firstLevelConns, "select id,name,parent_id from t_conn where (parent_id = '' or parent_id is null)")
	if err != nil {
		log.Printf("[ConnBaseTreeByService] 查询一级连接失败: %v", err)
		return firstLevel
	}
	for _, c := range firstLevelConns {
		name := ""
		if c.Name != nil {
			name = *c.Name
		}
		firstLevel = append(firstLevel, &conn.Tree{Label: name, Parent: c.ParentId, Id: c.Id, Type: conn.TREE_NODE_TYPE_CONN})
	}
	return firstLevel
}
