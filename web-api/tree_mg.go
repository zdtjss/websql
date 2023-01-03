package webapi

import (
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
	db.Exec("delete from t_tree where id = ?", r.Form.Get("id"))
	utils.WriteJson(w, "")
}

func findByParent(parentId string) []*Tree {
	treeList := []*DirTree{}
	err := db.Select(&treeList, "select * from t_tree where parent = ?", parentId)
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
		if cfg.Parent == "" {
			firstLevel = append(firstLevel, cfg)
		}
	}
	for _, cfg := range firstLevel {
		cfg.Children = findChild(cfg, tree)
	}
	utils.WriteJson(w, firstLevel)
}

func findChild(curNode *Tree, nodes []*Tree) []*Tree {
	child := make([]*Tree, 0)
	for _, cfg := range nodes {
		if cfg.Parent == curNode.Label {
			child = append(child, cfg)
			cfg.Children = findChild(cfg, nodes)
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

	stmt, err := tx.Prepare("insert into t_tree (label, parent) values (?, ?)")
	utils.Panicln(err)
	for _, t := range planeDir {
		stmt.Exec(&t.Label, &t.Parent)
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
		t.Parent = p.Label
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
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT,
			parent TEXT
		  );
		`
	db.Exec(sql_table)
}

type DirTree struct {
	Id       string     `json:"id"`
	Label    string     `json:"label"`
	Parent   string     `json:"parent"`
	Children []*DirTree `json:"children"`
}
