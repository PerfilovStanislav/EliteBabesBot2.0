package main

type Link struct {
	Id          int
	Link        string `db:"link"`
	Status      int
	Model       string
	Description string
	ChatId      int `db:"chat_id"`
}

type Media struct {
	LinkId    int    `db:"link_id"`
	FileId    string `db:"file_id"`
	MessageId int    `db:"message_id"`
}
