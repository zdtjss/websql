package permission

import (
	"time"

	"websql/internal/app/admin"
	"websql/internal/app/conn"
	"websql/internal/database"
	"websql/internal/logger"
	"websql/internal/pkg/idgen"
	"websql/internal/pkg/jsonutil"

	"github.com/gin-gonic/gin"
)

func SaveTree(c *gin.Context) {
	admin.CheckAdminPower(c)
	tree := []*DirTree{}
	jsonutil.UnmarshalJson(c.Request.Body, &tree)
	doTreeInsert(tree)
}

func DelTreeNode(c *gin.Context) {
	admin.CheckAdminPower(c)
	database.Mngtdb.Exec("delete from t_tree where id = ?", c.PostForm("id"))
	jsonutil.WriteJson(c.Writer, "")
}

func ListDirTree(c *gin.Context) {
	admin.CheckAdminPower(c)
	treeList := []*DirTree{}
	err := database.Mngtdb.Select(&treeList, "select * from t_tree")
	logger.PanicErr(err)
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
	jsonutil.WriteJson(c.Writer, firstLevel)
}

func ConnBaseTree(c *gin.Context) {
	admin.CheckAdminPower(c)

	treeList := []*DirTree{}
	err := database.Mngtdb.Select(&treeList, "select * from t_tree")
	logger.PanicErr(err)
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
	err = database.Mngtdb.Select(&firstLevelConns, "select id,name,parent_id from t_conn where (parent_id = '' or parent_id is null)")
	logger.PanicErr(err)
	for _, c := range firstLevelConns {
		name := ""
		if c.Name != nil {
			name = *c.Name
		}
		firstLevel = append(firstLevel, &conn.Tree{Label: name, Parent: c.ParentId, Id: c.Id, Type: conn.TREE_NODE_TYPE_CONN})
	}
	jsonutil.WriteJson(c.Writer, firstLevel)
}

func findChild(curNode *conn.Tree, nodes []*conn.Tree, connMap map[string][]*conn.ConnCfgBase) []*conn.Tree {
	childConn := make([]*conn.Tree, 0)
	conns, ok := connMap[curNode.Id]
	if ok {
		for _, c := range conns {
			name := ""
			if c.Name != nil {
				name = *c.Name
			}
			childConn = append(childConn, &conn.Tree{Label: name, Parent: c.ParentId, Id: c.Id, Type: conn.TREE_NODE_TYPE_CONN})
		}
	}
	curNode.Children = append(curNode.Children, childConn...)
	child := make([]*conn.Tree, 0)
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

	tx, err := database.Mngtdb.Beginx()
	logger.PanicErr(err)
	defer tx.Rollback()

	tx.Exec("delete from t_tree")

	stmt, err := tx.Prepare("insert into t_tree (id, label, parent) values (?, ?, ?)")
	logger.PanicErr(err)
	for _, t := range planeDir {
		id := t.Id
		if id == "" {
			time.Sleep(3 * time.Millisecond)
			id = idgen.RandomStr()
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

func listConnBase() map[string][]*conn.ConnCfgBase {
	cfgList := []*conn.ConnCfgBase{}
	err := database.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	logger.PanicErr(err)
	rolePowerMap := make(map[string][]*conn.ConnCfgBase, len(cfgList))
	for _, c := range cfgList {
		v, ok := rolePowerMap[c.ParentId]
		if !ok {
			v = []*conn.ConnCfgBase{}
		}
		v = append(v, c)
		rolePowerMap[c.ParentId] = v
	}
	return rolePowerMap
}

func ListConnBaseFromDB() []*conn.ConnCfgBase {
	cfgList := []*conn.ConnCfgBase{}
	database.Mngtdb.Select(&cfgList, "select id,name,parent_id from t_conn")
	return cfgList
}