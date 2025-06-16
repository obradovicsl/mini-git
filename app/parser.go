package main

import "fmt"

// Parsers for each available command - check the command format and return required infos

func parseCatCmdArgs(args []string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git cat-file <flag> <object_hash>")
	}

	objectFlag, objectHash := args[0], args[1]

	if objectFlag != "-t" && objectFlag != "-s" && objectFlag != "-p" {
		return "", "", fmt.Errorf("use: <flag> shold be -t or -s or -p")
	}

	return objectHash, objectFlag, nil
}

func parseHashObjectCmdArgs(args []string) (string, string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", "", fmt.Errorf("use: git hash-object <flag> <object_path>")
	}

	var flag string
	var path string
	if len(args) == 2 {
		flag = args[0]
		path = args[1]
	} else if len(args) == 1 {
		flag = ""
		path = args[0]
	}

	return path, flag, nil
}

func parseLsTreeCmdArgs(args []string) (string, string, error) {
	if len(args) != 1 && len(args) != 2 {
		return "", "", fmt.Errorf("use: git ls-tree <flag> <tree_path>")
	}

	var flag string
	var treeHash string
	if len(args) == 2 {
		flag = args[0]
		treeHash = args[1]
	} else if len(args) == 1 {
		flag = ""
		treeHash = args[0]
	}

	return treeHash, flag, nil
}

func parseCommitTreeCmdArgs(args []string) (string, string, string, error) {
	if len(args) != 3 && len(args) != 5 {
		return "", "", "", fmt.Errorf("use: git commit-tree <HASH> -p <HASH> -m <message>")
	}

	var message string
	var treeHash string
	var parentSHA string
	if len(args) == 3 {
		treeHash = args[0]
		message = args[2]
		parentSHA = ""
	} else if len(args) == 5 {
		treeHash = args[0]
		parentSHA = args[2]
		message = args[4]
	}

	return treeHash, message, parentSHA, nil
}

func parseCloneCmdArgs(args []string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("use: git clone <URL> <some_dir>")
	}

	var url string
	var directory string

	url = args[0]
	directory = args[1]

	return url, directory, nil
}
