package model

import "time"

type FileInfo struct {
	IsDir        bool       `json:"IsDir"`
	FullName     string     `json:"FullName"`
	Ext          string     `json:"Ext"`
	NameOnly     string     `json:"NameOnly"`
	SizeBytes    int64      `json:"SizeBytes"`
	SizeHuman    string     `json:"SizeHuman"`
	FullPath     string     `json:"FullPath"`
	FullPathOrig string     `json:"FullPathOrig"`
	ParentDir    string     `json:"ParentDir"`
	Created      time.Time  `json:"Created"`
	Updated      time.Time  `json:"Updated"`
	Perm         string     `json:"Perm"`
	Md5          string     `json:"Md5"`
	FileType     string     `json:"FileType"`
	ChildCount   int        `json:"ChildCount"`
	Children     []FileInfo `json:"Children,omitempty"`
}
