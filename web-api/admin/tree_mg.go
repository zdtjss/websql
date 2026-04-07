package admin

import (
	"bytes"
	"go-web/config"
	"go-web/logutils"
	"go-web/utils"
	"time"

	"github.com/gin-gonic/gin"
)

func SaveTree(c *gin.Context) {
	CheckAdminPower(c)
	tree := []*DirTree{}
	utils.UnmarshalJson(c.Request.Body, &tree)
	doTreeInsert(tree)
}

func DelTreeNode(c *gin.Context) {
	CheckAdminPower(c)
	config.Mngtdb.Exec("delete from t_tree where id = ?", c.PostForm("id"))
	utils.WriteJson(c.Writer, "")
}

func findByParent(parentId string, userPower *UserPower) []*Tree {
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_tree where ")
	if parentId == "" {
		sql.WriteString(" (parent is null or parent = '')")
	} else {
		param = append(param, parentId)
		sql.WriteString(" parent = ?")
	}
	// 注意：不对目录做 appendPmsn 过滤，目录 ID 不在 Power 列表中
	treeList := []*DirTree{}
	err := config.Mngtdb.Select(&treeList, sql.String(), param...)
	logutils.PanicErr(err)
	tree := make([]*Tree, len(treeList))
	for i, cfg := range treeList {
		tree[i] = &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR}
	}
	return tree
}

// findByParentWithPermission 查询目录节点，只返回包含用户有权限链接的目录。
// 权限逻辑参考 ListUserConn：通过 appendPmsn 过滤 t_conn.id IN (userPower.Power)。
// 目录 ID 不在权限列表中，所以先查所有目录，再检查每个目录下是否有授权链接。
func findByParentWithPermission(parentId string, userPower *UserPower) []*Tree {
	// 非远程模式不做权限过滤
	if !config.Cfg.IsRemote {
		return findByParent(parentId, userPower)
	}

	// 1. 查出当前层级的所有目录
	allDirs := findByParent(parentId, userPower)
	if len(allDirs) == 0 {
		return allDirs
	}

	// 2. 用户无权限则返回空
	if userPower == nil || len(userPower.Power) == 0 {
		return []*Tree{}
	}

	// 3. 查出用户有权限的所有链接的 parent_id
	connParam := []any{}
	connSQL := bytes.Buffer{}
	connSQL.WriteString("select id, parent_id from t_conn where 1 = 1 ")
	appendPmsn(&connSQL, "id", &connParam, userPower)

	type connParent struct {
		Id       string `db:"id"`
		ParentId string `db:"parent_id"`
	}
	connList := []connParent{}
	err := config.Mngtdb.Select(&connList, connSQL.String(), connParam...)
	logutils.PanicErr(err)

	// 4. 构建「有授权链接的目录 ID」集合
	dirsWithConn := make(map[string]bool)
	for _, conn := range connList {
		if conn.ParentId != "" {
			dirsWithConn[conn.ParentId] = true
		}
	}

	// 5. 向上传播：子目录有授权链接 → 父目录也应显示
	allTreeNodes := []*DirTree{}
	config.Mngtdb.Select(&allTreeNodes, "select * from t_tree")
	parentMap := make(map[string]string) // id → parent
	for _, node := range allTreeNodes {
		parentMap[node.Id] = node.Parent
	}
	toPropagate := make([]string, 0, len(dirsWithConn))
	for dirId := range dirsWithConn {
		toPropagate = append(toPropagate, dirId)
	}
	for len(toPropagate) > 0 {
		var next []string
		for _, dirId := range toPropagate {
			if pid, ok := parentMap[dirId]; ok && pid != "" && !dirsWithConn[pid] {
				dirsWithConn[pid] = true
				next = append(next, pid)
			}
		}
		toPropagate = next
	}

	// 6. 过滤：只保留有授权链接（直接或间接）的目录
	filtered := make([]*Tree, 0, len(allDirs))
	for _, dir := range allDirs {
		if dirsWithConn[dir.Id] {
			filtered = append(filtered, dir)
		}
	}
	return filtered
}

func ListDirTree(c *gin.Context) {
	CheckAdminPower(c)
	treeList := []*DirTree{}
	err := config.Mngtdb.Select(&treeList, "select * from t_tree")
	logutils.PanicErr(err)
	tree := []*Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*Tree{}
	for _, cfg := range tree {
		if cfg.Parent == "" {
			firstLevel = append(firstLevel, cfg)
		}
	}
	for _, cfg := range firstLevel {
		cfg.Children = findChild(cfg, tree, map[string][]*ConnCfgBase{})
	}
	utils.WriteJson(c.Writer, firstLevel)
}

func ConnBaseTree(c *gin.Context) {

	CheckAdminPower(c)

	treeList := []*DirTree{}
	err := config.Mngtdb.Select(&treeList, "select * from t_tree")
	logutils.PanicErr(err)
	tree := []*Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*Tree{}
	for _, cfg := range tree {
		if cfg.Parent == "" {
			firstLevel = append(firstLevel, cfg)
		}
	}
	connMap := listConnBase()
	for _, cfg := range firstLevel {
		cfg.Children = append(cfg.Children, findChild(cfg, tree, connMap)...)
	}
	firstLevelConns := []*ConnCfgBase{}
	err = config.Mngtdb.Select(&firstLevelConns, "select id,name,parent_id from t_conn where (parent_id = '' or parent_id is null)")
	logutils.PanicErr(err)
	for _, conn := range firstLevelConns {
		name := ""
		if conn.Name != nil {
			name = *conn.Name
		}
		firstLevel = append(firstLevel, &Tree{Label: name, Parent: conn.ParentId, Id: conn.Id, Type: TREE_NODE_TYPE_CONN})
	}
	utils.WriteJson(c.Writer, firstLevel)
}

func findChild(curNode *Tree, nodes []*Tree, connMap map[string][]*ConnCfgBase) []*Tree {
	childConn := make([]*Tree, 0)
	conns, ok := connMap[curNode.Id]
	if ok {
		for _, conn := range conns {
			name := ""
			if conn.Name != nil {
				name = *conn.Name
			}
			childConn = append(childConn, &Tree{Label: name, Parent: conn.ParentId, Id: conn.Id, Type: TREE_NODE_TYPE_CONN})
		}
	}
	curNode.Children = append(curNode.Children, childConn...)
	child := make([]*Tree, 0)
	for _, cfg := range nodes {
		if cfg.Parent == curNode.Id {
			child = append(child, cfg)
			cfg.Children = append(cfg.Children, findChild(cfg, nodes, connMap)...)
		}
	}
	return child
}

func doTreeInsert(tree []*DirTree) {

	planeDir := expendDirTreeAll(tree)

	tx, err := config.Mngtdb.Beginx()
	logutils.PanicErr(err)
	defer tx.Rollback()

	tx.Exec("delete from t_tree")

	stmt, err := tx.Prepare("insert into t_tree (id, label, parent) values (?, ?, ?)")
	logutils.PanicErr(err)
	for _, t := range planeDir {
		id := t.Id
		if id == "" {
			time.Sleep(3 * time.Millisecond)
			id = utils.RandomStr()
		}
		stmt.Exec(id, &t.Label, &t.Parent)
	}
	tx.Commit()
}

func expendDirTreeAll(root []*DirTree) []*DirTree {
	all := []*DirTree{}
	for _, t := range root {
		all = append(all, t)
		all = append(all, expendDirTree(t)...)
	}
	return all
}

func expendDirTree(p *DirTree) []*DirTree {
	child := []*DirTree{}
	for _, t := range p.Children {
		t.Parent = p.Id
		child = append(child, t)
		if t.Children != nil {
			child = append(child, expendDirTree(t)...)
		}
	}
	return child
}

type DirTree struct {
	Id       string     `json:"id" db:"id"`
	Label    string     `json:"label" db:"label"`
	Parent   string     `json:"parent" db:"parent"`
	Children []*DirTree `json:"children"`
}
