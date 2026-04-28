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
