package main

import "fmt"

// All types that our program uses

type ObjectType int

func (objType ObjectType) String() string {
	switch objType {
	case OBJ_TREE:
		return "tree"
	case OBJ_COMMIT:
		return "commit"
	case OBJ_BLOB:
		return "blob"
	case OBJ_TAG:
		return "tag"
	case OBJ_OFS_DELTA:
		return "ofs_delta"
	case OBJ_REF_DELTA:
		return "ref_delta"
	default:
		return ""
	}
}

func ObjectTypeFromString(s string) (ObjectType, error) {
	switch s {
	case "tree":
		return OBJ_TREE, nil
	case "commit":
		return OBJ_COMMIT, nil
	case "blob":
		return OBJ_BLOB, nil
	case "tag":
		return OBJ_TAG, nil
	default:
		return 0, fmt.Errorf("unknown ObjectType: " + s)
	}
}

const (
	OBJ_COMMIT    = 1
	OBJ_TREE      = 2
	OBJ_BLOB      = 3
	OBJ_TAG       = 4
	OBJ_OFS_DELTA = 6
	OBJ_REF_DELTA = 7
)

type IndexEntry struct {
	Path string
	Hash []byte
	Mode uint32
}

type TreeNode struct {
	Name     string
	IsDir    bool
	Hash     []byte
	Mode     uint32
	Children map[string]*TreeNode
}

type GitObject struct {
	Type        ObjectType
	Data        []byte
	BaseObjHash string
	Size        uint64
}
