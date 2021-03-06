package postgres

import (
	"errors"
	"fmt"
	"math"
	"time"

	"database/sql"

	"github.com/getfider/fider/app/models"
	"github.com/getfider/fider/app/pkg/dbx"
	"github.com/gosimple/slug"
)

type dbIdea struct {
	ID               int            `db:"id"`
	Number           int            `db:"number"`
	Title            string         `db:"title"`
	Slug             string         `db:"slug"`
	Description      string         `db:"description"`
	CreatedOn        time.Time      `db:"created_on"`
	User             *dbUser        `db:"user"`
	ViewerSupported  bool           `db:"viewer_supported"`
	TotalSupporters  int            `db:"supporters"`
	TotalComments    int            `db:"comments"`
	RecentSupporters int            `db:"recent_supporters"`
	RecentComments   int            `db:"recent_comments"`
	Status           int            `db:"status"`
	Response         sql.NullString `db:"response"`
	RespondedOn      dbx.NullTime   `db:"response_date"`
	ResponseUser     *dbUser        `db:"response_user"`
	OriginalNumber   sql.NullInt64  `db:"original_number"`
	OriginalTitle    sql.NullString `db:"original_title"`
	OriginalSlug     sql.NullString `db:"original_slug"`
	OriginalStatus   sql.NullInt64  `db:"original_status"`
	Tags             []int64        `db:"tags"`
}

func (i *dbIdea) toModel() *models.Idea {
	ranking := float64((i.RecentSupporters*5)+(i.RecentComments*3)-1) / math.Pow((time.Since(i.CreatedOn).Hours()+2), 1.4)
	if math.IsNaN(ranking) {
		ranking = 0
	}

	idea := &models.Idea{
		ID:              i.ID,
		Number:          i.Number,
		Title:           i.Title,
		Slug:            i.Slug,
		Description:     i.Description,
		CreatedOn:       i.CreatedOn,
		ViewerSupported: i.ViewerSupported,
		TotalSupporters: i.TotalSupporters,
		TotalComments:   i.TotalComments,
		Status:          i.Status,
		User:            i.User.toModel(),
		Tags:            i.Tags,
		Ranking:         ranking,
	}

	if i.Response.Valid {
		idea.Response = &models.IdeaResponse{
			Text:        i.Response.String,
			RespondedOn: i.RespondedOn.Time,
			User:        i.ResponseUser.toModel(),
		}
		if idea.Status == models.IdeaDuplicate && i.OriginalNumber.Valid {
			idea.Response.Original = &models.OriginalIdea{
				Number: int(i.OriginalNumber.Int64),
				Slug:   i.OriginalSlug.String,
				Title:  i.OriginalTitle.String,
				Status: int(i.OriginalStatus.Int64),
			}
		}
	}
	return idea
}

func (i *dbIdea) toBasic() *models.BasicIdea {
	return &models.BasicIdea{
		ID:              i.ID,
		Number:          i.Number,
		Title:           i.Title,
		Slug:            i.Slug,
		TotalSupporters: i.TotalSupporters,
		Status:          i.Status,
	}
}

type dbComment struct {
	ID        int       `db:"id"`
	Content   string    `db:"content"`
	CreatedOn time.Time `db:"created_on"`
	User      *dbUser   `db:"user"`
}

func (c *dbComment) toModel() *models.Comment {
	return &models.Comment{
		ID:        c.ID,
		Content:   c.Content,
		CreatedOn: c.CreatedOn,
		User:      c.User.toModel(),
	}
}

// IdeaStorage contains read and write operations for ideas
type IdeaStorage struct {
	trx    *dbx.Trx
	tenant *models.Tenant
	user   *models.User
}

// NewIdeaStorage creates a new IdeaStorage
func NewIdeaStorage(trx *dbx.Trx) *IdeaStorage {
	return &IdeaStorage{
		trx: trx,
	}
}

// SetCurrentTenant to current context
func (s *IdeaStorage) SetCurrentTenant(tenant *models.Tenant) {
	s.tenant = tenant
}

// SetCurrentUser to current context
func (s *IdeaStorage) SetCurrentUser(user *models.User) {
	s.user = user
}

var (
	sqlSelectIdeasWhere = `SELECT i.id, 
																i.number, 
																i.title, 
																i.slug, 
																i.description, 
																i.created_on,
																i.supporters,
																(SELECT COUNT(*) FROM comments WHERE idea_id = i.id) as comments,
																(SELECT COUNT(*) FROM idea_supporters WHERE idea_id = i.id AND created_on > CURRENT_DATE - INTERVAL '30 days') AS recent_supporters,
																(SELECT COUNT(*) FROM comments WHERE idea_id = i.id AND created_on > CURRENT_DATE - INTERVAL '30 days') AS recent_comments,
																i.status, 
																u.id AS user_id, 
																u.name AS user_name, 
																u.email AS user_email,
																u.role AS user_role,
																i.response,
																i.response_date,
																r.id AS response_user_id, 
																r.name AS response_user_name, 
																r.email AS response_user_email, 
																r.role AS response_user_role,
																d.number AS original_number,
																d.title AS original_title,
																d.slug AS original_slug,
																d.status AS original_status,
																array_remove(array_agg(t.id), NULL) AS tags,
																COALESCE(%s, false) AS viewer_supported
													FROM ideas i
													INNER JOIN users u
													ON u.id = i.user_id
													LEFT JOIN users r
													ON r.id = i.response_user_id
													LEFT JOIN idea_tags it
													ON it.idea_id = i.id
													LEFT JOIN ideas d
													ON d.id = i.original_id
													LEFT JOIN tags t
													ON t.id = it.tag_id
													%s
													WHERE %s
													GROUP BY i.id, u.id, r.id, d.id`
)

func (s *IdeaStorage) getIdeaQuery(filter string) string {
	viewerSupportedSubQuery := "null"
	if s.user != nil {
		viewerSupportedSubQuery = fmt.Sprintf("(SELECT true FROM idea_supporters WHERE idea_id = i.id AND user_id = %d)", s.user.ID)
	}
	tagCondition := `AND t.is_public = true`
	if s.user != nil && s.user.IsCollaborator() {
		tagCondition = ``
	}
	return fmt.Sprintf(sqlSelectIdeasWhere, viewerSupportedSubQuery, tagCondition, filter)
}

func (s *IdeaStorage) getSingle(query string, args ...interface{}) (*models.Idea, error) {
	idea := dbIdea{}

	if err := s.trx.Get(&idea, query, args...); err != nil {
		return nil, err
	}

	return idea.toModel(), nil
}

// GetByID returns idea by given id
func (s *IdeaStorage) GetByID(ideaID int) (*models.Idea, error) {
	return s.getSingle(s.getIdeaQuery("i.tenant_id = $1 AND i.id = $2"), s.tenant.ID, ideaID)
}

// GetBySlug returns idea by tenant and slug
func (s *IdeaStorage) GetBySlug(slug string) (*models.Idea, error) {
	return s.getSingle(s.getIdeaQuery("i.tenant_id = $1 AND i.slug = $2"), s.tenant.ID, slug)
}

// GetByNumber returns idea by tenant and number
func (s *IdeaStorage) GetByNumber(number int) (*models.Idea, error) {
	return s.getSingle(s.getIdeaQuery("i.tenant_id = $1 AND i.number = $2"), s.tenant.ID, number)
}

// GetAll returns all tenant ideas
func (s *IdeaStorage) GetAll() ([]*models.Idea, error) {
	var ideas []*dbIdea
	err := s.trx.Select(&ideas, s.getIdeaQuery("i.tenant_id = $1 AND i.status != $2"), s.tenant.ID, models.IdeaDuplicate)
	if err != nil {
		return nil, err
	}

	var result = make([]*models.Idea, len(ideas))
	for i, idea := range ideas {
		result[i] = idea.toModel()
	}
	return result, nil
}

// GetAllBasic returns all tenant ideas in a Basic model
func (s *IdeaStorage) GetAllBasic() ([]*models.BasicIdea, error) {
	var ideas []*dbIdea
	innerQuery := s.getIdeaQuery("i.tenant_id = $1 AND i.status != $2")
	query := fmt.Sprintf("SELECT id, number, title, slug, supporters, status FROM (%s) AS q", innerQuery)
	err := s.trx.Select(&ideas, query, s.tenant.ID, models.IdeaDuplicate)
	if err != nil {
		return nil, err
	}

	var result = make([]*models.BasicIdea, len(ideas))
	for i, idea := range ideas {
		result[i] = idea.toBasic()
	}
	return result, nil
}

// GetCommentsByIdea returns all coments from given idea
func (s *IdeaStorage) GetCommentsByIdea(number int) ([]*models.Comment, error) {
	comments := []*dbComment{}
	err := s.trx.Select(&comments,
		`SELECT c.id, 
				c.content, 
				c.created_on, 
				u.id AS user_id, 
				u.name AS user_name,
				u.email AS user_email,
				u.role AS user_role
		FROM comments c
		INNER JOIN ideas i
		ON i.id = c.idea_id
		INNER JOIN users u
		ON u.id = c.user_id
		WHERE i.number = $1
		AND i.tenant_id = $2
		ORDER BY c.created_on ASC`, number, s.tenant.ID)
	if err != nil {
		return nil, err
	}

	var result = make([]*models.Comment, len(comments))
	for i, comment := range comments {
		result[i] = comment.toModel()
	}
	return result, nil
}

// Update given idea
func (s *IdeaStorage) Update(number int, title, description string) (*models.Idea, error) {
	err := s.trx.Execute(`UPDATE ideas SET title = $1, slug = $2, description = $3 
												WHERE number = $4 AND tenant_id = $5`, title, slug.Make(title), description, number, s.tenant.ID)
	if err != nil {
		return nil, err
	}

	return s.GetByNumber(number)
}

// Add a new idea in the database
func (s *IdeaStorage) Add(title, description string, userID int) (*models.Idea, error) {
	var id int
	row := s.trx.QueryRow(`INSERT INTO ideas (title, slug, number, description, tenant_id, user_id, created_on, supporters, status) 
						VALUES ($1, $2, (SELECT COALESCE(MAX(number), 0) + 1 FROM ideas i WHERE i.tenant_id = $4), $3, $4, $5, $6, 0, 0) 
						RETURNING id`, title, slug.Make(title), description, s.tenant.ID, userID, time.Now())
	if err := row.Scan(&id); err != nil {
		return nil, err
	}

	return s.GetByID(id)
}

// AddComment places a new comment on an idea
func (s *IdeaStorage) AddComment(number int, content string, userID int) (int, error) {
	idea, err := s.GetByNumber(number)
	if err != nil {
		return 0, err
	}

	var id int
	if err := s.trx.QueryRow("INSERT INTO comments (idea_id, content, user_id, created_on) VALUES ($1, $2, $3, $4) RETURNING id", idea.ID, content, userID, time.Now()).Scan(&id); err != nil {
		return 0, err
	}

	return id, nil
}

// AddSupporter adds user to idea list of supporters
func (s *IdeaStorage) AddSupporter(number, userID int) error {
	idea, err := s.GetByNumber(number)
	if err != nil {
		return err
	}

	if !idea.CanBeSupported() {
		return nil
	}

	alreadySupported, err := s.trx.Exists("SELECT 1 FROM idea_supporters WHERE user_id = $1 AND idea_id = $2", userID, idea.ID)
	if err != nil {
		return err
	}

	if alreadySupported {
		return nil
	}

	if err := s.trx.Execute(`UPDATE ideas SET supporters = supporters + 1 WHERE id = $1`, idea.ID); err != nil {
		return err
	}

	return s.trx.Execute(`INSERT INTO idea_supporters (user_id, idea_id, created_on) VALUES ($1, $2, $3)`, userID, idea.ID, time.Now())
}

// RemoveSupporter removes user from idea list of supporters
func (s *IdeaStorage) RemoveSupporter(number, userID int) error {
	idea, err := s.GetByNumber(number)
	if err != nil {
		return err
	}

	if !idea.CanBeSupported() {
		return nil
	}

	didSupport, err := s.trx.Exists("SELECT 1 FROM idea_supporters WHERE user_id = $1 AND idea_id = $2", userID, idea.ID)
	if err != nil {
		return err
	}

	if !didSupport {
		return nil
	}

	if err := s.trx.Execute(`UPDATE ideas SET supporters = supporters - 1 WHERE id = $1`, idea.ID); err != nil {
		return err
	}

	return s.trx.Execute(`DELETE FROM idea_supporters WHERE user_id = $1 AND idea_id = $2`, userID, idea.ID)
}

// SetResponse changes current idea response
func (s *IdeaStorage) SetResponse(number int, text string, userID, status int) error {
	if status == models.IdeaDuplicate {
		return errors.New("Use MarkAsDuplicate to change an idea status to Duplicate")
	}

	idea, err := s.GetByNumber(number)
	if err != nil {
		return err
	}

	respondedOn := time.Now()
	if idea.Status == status && idea.Response != nil {
		respondedOn = idea.Response.RespondedOn
	}
	return s.trx.Execute(`
	UPDATE ideas 
	SET response = $3, original_id = NULL, response_date = $4, response_user_id = $5, status = $6 
	WHERE id = $1 and tenant_id = $2
	`, idea.ID, s.tenant.ID, text, respondedOn, userID, status)
}

// MarkAsDuplicate set idea as a duplicate of another idea
func (s *IdeaStorage) MarkAsDuplicate(number, originalNumber, userID int) error {
	idea, err := s.GetByNumber(number)
	if err != nil {
		return err
	}
	original, err := s.GetByNumber(originalNumber)
	if err != nil {
		return err
	}

	respondedOn := time.Now()
	if idea.Status == models.IdeaDuplicate && idea.Response != nil {
		respondedOn = idea.Response.RespondedOn
	}

	users, err := s.trx.QueryIntArray("SELECT user_id FROM idea_supporters WHERE idea_id = $1", idea.ID)
	if err != nil {
		return err
	}

	for _, u := range users {
		if err := s.AddSupporter(original.Number, u); err != nil {
			return err
		}
	}

	return s.trx.Execute(`
	UPDATE ideas 
	SET response = '', original_id = $3, response_date = $4, response_user_id = $5, status = $6 
	WHERE id = $1 and tenant_id = $2
	`, idea.ID, s.tenant.ID, original.ID, respondedOn, userID, models.IdeaDuplicate)
}

// SupportedBy returns a list of Idea ID supported by given user
func (s *IdeaStorage) SupportedBy(userID int) ([]int, error) {
	return s.trx.QueryIntArray("SELECT idea_id FROM idea_supporters WHERE user_id = $1", userID)
}
