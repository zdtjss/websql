package webapi

import (
	"go-web/utils"
	"net/http"
)

func SaveTree(w http.ResponseWriter, r *http.Request) {
	tree := &DirTree{}
	utils.UnmarshalJson(r.Body, tree)
	if tree.Id == "" {
		doTreeInsert(tree)
	} else {
		doTreeUpdate(tree)
	}
}

func DelTreeNode(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	db.Exec("delete from t_tree where id = ?", r.Form.Get("id"))
	utils.WriteJson(w, "")
}

func ListDirTree() []*Tree {

	treeList := []DirTree{}
	err := db.Select(&treeList, "select * from t_tree")
	utils.Panicln(err)
	tree := make([]*Tree, len(treeList))
	for i, cfg := range treeList {
		if cfg.Parent == "" {
			tree[i] = &Tree{Label: cfg.Name, Parent: cfg.Parent, Data: map[string]any{"id": cfg.Id}, Type: TREE_NODE_TYPE_DIR}
		}
	}
	for _, cfg := range tree {
		cfg.Children = findChild(cfg, tree)
	}
	return tree
}

func findChild(curNode *Tree, nodes []*Tree) []*Tree {
	child := make([]*Tree, 0)
	for _, cfg := range nodes {
		if cfg.Parent == curNode.Data["id"] {
			child = append(child, cfg)
		} else {
			curNode.Children = findChild(cfg, nodes)
		}
	}
	return child
}

func doTreeInsert(tree *DirTree) {

	initConfigTable()

	stmt, _ := db.Prepare("insert into t_tree (name, parent) values (?, ?)")
	stmt.Exec(&tree.Name, &tree.Parent)
}

func doTreeUpdate(tree *DirTree) {
	stmt, _ := db.Prepare("update t_tree set name = ?, parent = ?where id = ?")
	stmt.Exec(&tree.Name, &tree.Name, &tree.Parent, &tree.Id)
}

func initTreeTable() {
	//创建表
	sql_table := `
		CREATE TABLE IF NOT EXISTS t_tree (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			parent TEXT,
		  );
		`
	db.Exec(sql_table)
}

type DirTree struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
}
