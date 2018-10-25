package main

import (
	"database/sql"
	"gopkg.in/guregu/null.v3"
	"time"
)

type category struct {
	Name string `json:"name"`
}

func getCategoriesFromDB(db *sql.DB) ([]category, error) {
	rows, err := db.Query("SELECT name FROM categories ORDER BY name ASC")

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	categories := []category{}
	for rows.Next() {
		var c category
		if err := rows.Scan(&c.Name); err != nil {
			return nil, err
		}
		categories = append(categories, c)
	}

	return categories, nil
}

type board struct {
	Name        string      `json:"name"`
	Description null.String `json:"description"`
	Category    null.String `json:"category"`
}

func getBoardsFromDB(db *sql.DB) ([]board, error) {
	rows, err := db.Query("SELECT boards.name, boards.description, categories.name FROM boards LEFT JOIN categories ON boards.category = categories.id ORDER BY boards.name ASC")

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	boards := []board{}
	for rows.Next() {
		var b board
		if err := rows.Scan(&b.Name, &b.Description, &b.Category); err != nil {
			return nil, err
		}
		boards = append(boards, b)
	}

	return boards, nil
}

func showCategoryFromDB(db *sql.DB, name string) ([]board, error) {
	rows, err := db.Query("SELECT boards.name, boards.description, categories.name FROM boards LEFT JOIN categories ON boards.category = categories.id WHERE categories.name = $1 ORDER BY boards.name ASC", name)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	boards := []board{}
	for rows.Next() {
		var b board
		if err := rows.Scan(&b.Name, &b.Description, &b.Category); err != nil {
			return nil, err
		}
		boards = append(boards, b)
	}

	return boards, nil
}

type boardReply struct {
	Board   board    `json:"board"`
	Threads []thread `json:"threads"`
}

type thread struct {
	PostNum         int         `json:"post_num"`
	Subject         null.String `json:"subject"`
	Author          string      `json:"author"`
	Time            time.Time   `json:"post_time"`
	NumReplies      int         `json:"num_replies"`
	LatestReply     null.Time   `json:"latest_reply_time"`
	Comment         string      `json:"comment"`
	SortLatestReply time.Time   `json:"-"`
}

type post struct {
	PostNum int       `json:"post_num"`
	Author  string    `json:"author"`
	Time    time.Time `json:"time"`
	Comment string    `json:"comment"`
}

func showBoardFromDB(db *sql.DB, name string) (boardReply, error) {
	var reply boardReply

	b_row := db.QueryRow(
		`SELECT boards.name, boards.description, boards.category
		FROM boards
		WHERE boards.name = $1`,
		name,
	)

	var b board
	if err := b_row.Scan(&b.Name, &b.Description, &b.Category); err != nil {
		return reply, err
	}

	rows, err := db.Query(
		`SELECT
			threads.post_num,
			threads.subject,
			threads.author,
			threads.time,
			(SELECT COUNT(*) FROM comments WHERE comments.reply_to = threads.id) AS num_replies,
			MAX(comments.time) AS latest_reply,
			threads.comment,
			CASE
				WHEN MAX(comments.time) IS NOT NULL AND COUNT(*) >= (SELECT bump_limit FROM boards WHERE name = $1)  THEN (SELECT comments.time FROM comments OFFSET (SELECT bump_limit FROM boards WHERE name = $1) LIMIT 1)
				WHEN MAX(comments.time) IS NOT NULL THEN MAX(comments.time)
				ELSE MAX(threads.time)
			END AS sort_latest_reply
		FROM threads
		LEFT JOIN comments ON threads.id = comments.reply_to
		WHERE threads.board_id = (SELECT id FROM boards WHERE name = $1)
		GROUP BY threads.id
		ORDER BY sort_latest_reply DESC`,
		name,
	)

	if err != nil {
		return reply, err
	}
	defer rows.Close()

	threads := []thread{}
	for rows.Next() {
		var t thread
		if err := rows.Scan(&t.PostNum, &t.Subject, &t.Author, &t.Time, &t.NumReplies, &t.LatestReply, &t.Comment, &t.SortLatestReply); err != nil {
			return reply, err
		}
		threads = append(threads, t)
	}

	reply = boardReply{b, threads}
	return reply, nil
}

type threadReply struct {
	Thread thread `json:"op"`
	Posts  []post `json:"posts"`
}

func showThreadFromDB(db *sql.DB, board string, thread_num int) (threadReply, error) {
	var reply threadReply

	t_row := db.QueryRow(
		`SELECT
			threads.post_num,
			threads.subject,
			threads.author,
			threads.time,
			(SELECT COUNT(*) FROM comments WHERE comments.reply_to = threads.id) AS num_replies,
			MAX(comments.time) AS latest_reply,
			threads.comment,
			CASE
				WHEN MAX(comments.time) IS NOT NULL AND COUNT(*) >= (SELECT bump_limit FROM boards WHERE name = $1)  THEN (SELECT comments.time FROM comments OFFSET (SELECT bump_limit FROM boards WHERE name = $1) LIMIT 1)
				WHEN MAX(comments.time) IS NOT NULL THEN MAX(comments.time)
				ELSE MAX(threads.time)
			END AS sort_latest_reply
		FROM threads
		LEFT JOIN comments ON threads.id = comments.reply_to
		WHERE threads.board_id = (SELECT id FROM boards WHERE name = $1)
		AND threads.post_num = $2
		GROUP BY threads.id
		ORDER BY sort_latest_reply DESC`,
		board,
		thread_num,
	)

	var t thread
	if err := t_row.Scan(&t.PostNum, &t.Subject, &t.Author, &t.Time, &t.NumReplies, &t.LatestReply, &t.Comment, &t.SortLatestReply); err != nil {
		return reply, err
	}

	c_rows, err := db.Query(
		`SELECT comments.post_num, comments.author, comments.time, comments.comment
		FROM comments
		INNER JOIN threads ON comments.reply_to = threads.id
		WHERE threads.board_id = (SELECT id FROM boards WHERE name = $1)
		AND comments.reply_to = (SELECT threads.id FROM threads INNER JOIN boards ON threads.board_id = boards.id WHERE boards.name = $1 AND threads.post_num = $2)
		ORDER BY post_num ASC`,
		board,
		thread_num,
	)

	if err != nil {
		return reply, err
	}
	defer c_rows.Close()

	posts := []post{}
	for c_rows.Next() {
		var p post
		if err := c_rows.Scan(&p.PostNum, &p.Author, &p.Time, &p.Comment); err != nil {
			return reply, err
		}
		posts = append(posts, p)
	}

	reply = threadReply{t, posts}
	return reply, nil
}

func makeThreadInDB(db *sql.DB, board string, comment string, author string, subject string) (int, error) {
	rows, err := db.Query(
		`INSERT INTO threads (board_id, comment, author, subject)
		VALUES(
			(SELECT id FROM boards WHERE name = $1),
			$2,
			$3,
			CASE WHEN $4 = '' THEN NULL ELSE $4 END
		)
		RETURNING post_num`,
		board,
		comment,
		author,
		subject,
	)

	if err != nil {
		return 0, err
	}

	post_nums := []int{}
	for rows.Next() {
		var i int
		if err := rows.Scan(&i); err != nil {
			return 0, err
		}
		post_nums = append(post_nums, i)
	}

	return post_nums[0], nil
}

func makePostInDB(db *sql.DB, board string, thread int, comment string, author string) (int, error) {
	rows, err := db.Query(
		`INSERT INTO comments (reply_to, comment, author)
		VALUES(
			(SELECT threads.id FROM threads INNER JOIN boards ON threads.board_id = boards.id WHERE boards.name = $1 AND threads.post_num = $2),
			$3,
			$4
		)
		RETURNING post_num`,
		board,
		thread,
		comment,
		author,
	)

	if err != nil {
		return 0, err
	}

	post_nums := []int{}
	for rows.Next() {
		var i int
		if err := rows.Scan(&i); err != nil {
			return 0, err
		}
		post_nums = append(post_nums, i)
	}

	return post_nums[0], nil
}

func checkIsOp(db *sql.DB, board string, thread int) (bool, error) {
	rows, err := db.Query(
		`SELECT post_num
		FROM threads
		INNER JOIN boards ON threads.board_id = boards.id
		WHERE threads.board_id = (SELECT id FROM boards WHERE name = $1)
		AND threads.post_num = $2`,
		board,
		thread,
	)

	if err != nil {
		return false, err
	}

	posts := []post{}
	for rows.Next() {
		var p post
		if err := rows.Scan(&p.PostNum); err != nil {
			return false, err
		}
		posts = append(posts, p)
	}

	if len(posts) == 0 {
		return false, nil
	} else {
		return true, nil
	}
}
