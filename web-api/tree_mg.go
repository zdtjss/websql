package webapi

import (
	"bytes"
	"go-web/utils"
	"net/http"
)

func SaveTree(w http.ResponseWriter, r *http.Request) {
	tree := []*DirTree{}
	utils.UnmarshalJson(r.Body, &tree)
	doTreeInsert(tree)
}

func DelTreeNode(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	db.Exec("delete from t_tree where id = ?", utils.AtoUint64(r.FormValue("id")))
	utils.WriteJson(w, "")
}

func findByParent(parentId string) []*Tree {
	param := []any{}
	sql := bytes.Buffer{}
	sql.WriteString("select * from t_tree where ")
	if parentId == "" {
		sql.WriteString(" parent is null or parent = 0")
	} else {
		param = append(param, utils.AtoUint64(parentId))
		sql.WriteString(" parent = ?")
	}
	treeList := []*DirTree{}
	err := db.Select(&treeList, sql.String(), param...)
	utils.Panicln(err)
	tree := make([]*Tree, len(treeList))
	for i, cfg := range treeList {
		tree[i] = &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR}
	}
	return tree
}

func ListDirTree(w http.ResponseWriter, r *http.Request) {

	treeList := []*DirTree{}
	err := db.Select(&treeList, "select * from t_tree")
	utils.Panicln(err)
	tree := []*Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*Tree{}
	for _, cfg := range tree {
		if cfg.Parent == 0 {
			firstLevel = append(firstLevel, cfg)
		}
	}
	for _, cfg := range firstLevel {
		cfg.Children = findChild(cfg, tree, map[uint64][]*ConnCfgBase{})
	}
	utils.WriteJson(w, firstLevel)
}

func ConnBaseTree(w http.ResponseWriter, r *http.Request) {
	treeList := []*DirTree{}
	err := db.Select(&treeList, "select * from t_tree")
	utils.Panicln(err)
	tree := []*Tree{}
	for _, cfg := range treeList {
		tree = append(tree, &Tree{Label: cfg.Label, Parent: cfg.Parent, Id: cfg.Id, Type: TREE_NODE_TYPE_DIR})
	}
	firstLevel := []*Tree{}
	for _, cfg := range tree {
		if cfg.Parent == 0 {
			firstLevel = append(firstLevel, cfg)
		}
	}
	connMap := listConnBase()
	for _, cfg := range firstLevel {
		cfg.Children = append(cfg.Children, findChild(cfg, tree, connMap)...)
	}
	utils.WriteJson(w, firstLevel)
}

func findChild(curNode *Tree, nodes []*Tree, connMap map[uint64][]*ConnCfgBase) []*Tree {
	childConn := make([]*Tree, 0)
	conns, ok := connMap[curNode.Id]
	if ok {
		for _, conn := range conns {
			childConn = append(childConn, &Tree{Label: conn.Name, Parent: conn.ParentId, Id: conn.Id, Type: TREE_NODE_TYPE_CONN})
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

	initTreeTable()

	planeDir := expendDirTreeAll(tree)

	tx, err := db.Beginx()
	utils.Panicln(err)
	defer tx.Rollback()

	tx.Exec("delete from t_tree")

	stmt, err := tx.Prepare("insert into t_tree (id, label, parent) values (?, ?, ?)")
	utils.Panicln(err)
	for _, t := range planeDir {
		stmt.Exec(utils.RandomInt64(), &t.Label, &t.Parent)
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

func initTreeTable() {
	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_tree (
			id BIGINT PRIMARY KEY,
			label TEXT,
			parent BIGINT
		  );
		`
	db.Exec(sql_table)
}

type DirTree struct {
	Id       uint64     `json:"id" db:"id"`
	Label    string     `json:"label" db:"label"`
	Parent   uint64     `json:"parent" db:"parent"`
	Children []*DirTree `json:"children"`
}
