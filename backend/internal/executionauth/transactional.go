package executionauth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"

	"automation-hub-backend/internal/infra"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AuthorizationPostCommitProjection projects a committed authorization
// receipt into the advisory life graph. It must only be invoked after tx commits.
type AuthorizationPostCommitProjection func(context.Context) Receipt

// PostgresTransactionApprovalResolver resolves mutable task-review authority
// through the same transaction that persists and consumes its authorization.
// The implementation must lock the durable review row before accepting it.
type PostgresTransactionApprovalResolver interface {
	ResolveInPostgresTransaction(
		context.Context,
		*gorm.DB,
		string,
		string,
		string,
	) (ResolvedApproval, error)
}

type transactionBoundApprovalResolver struct {
	transaction *gorm.DB
	resolver    PostgresTransactionApprovalResolver
}

func (r transactionBoundApprovalResolver) Resolve(
	ctx context.Context,
	ownerIdentity string,
	sourceID string,
	bindingDigest string,
) (ResolvedApproval, error) {
	return r.resolver.ResolveInPostgresTransaction(
		ctx,
		r.transaction,
		ownerIdentity,
		sourceID,
		bindingDigest,
	)
}

// AuthorizeAndConsumeInTransaction runs the existing authorization, mutable-
// evidence recheck, receipt persistence, and single-use consume path against
// the caller's PostgreSQL transaction. Any returned error requires the caller
// to roll back that transaction. The projection closure is advisory and must
// only run after a successful commit.
func (s *Service) AuthorizeAndConsumeInTransaction(
	ctx context.Context,
	tx *gorm.DB,
	request Request,
	consumer string,
	executionTarget string,
) (Receipt, AuthorizationPostCommitProjection, error) {
	if s == nil {
		return Receipt{}, nil, ErrPostgresTransactionRequired
	}
	repository, ok := s.repository.(*PostgresRepository)
	if !ok || repository == nil || !samePostgresTransaction(repository.DB, tx) {
		return Receipt{}, nil, ErrPostgresTransactionRequired
	}
	projectionDefaultContext := ctx
	bound, finish, err := infra.PostgresExecutionDB(ctx, tx)
	if err != nil {
		return Receipt{}, nil, err
	}
	defer finish()
	tx, ctx = bound, bound.Statement.Context
	ctx, err = infra.PostgresExecutionTransactionContext(ctx, tx)
	if err != nil {
		return Receipt{}, nil, err
	}
	tx = tx.WithContext(ctx)

	transactional := *s
	transactional.repository = NewPostgresRepository(tx)
	transactional.lifeGraph = nil
	if isTaskReviewApprovalSource(request.ApprovalSourceID) {
		resolver, ok := s.approvals.(PostgresTransactionApprovalResolver)
		if !ok || resolver == nil {
			return Receipt{}, nil, ErrPostgresTransactionRequired
		}
		transactional.approvals = transactionBoundApprovalResolver{
			transaction: tx,
			resolver:    resolver,
		}
	}

	receipt, err := transactional.authorizeAndConsumeCore(ctx, request, consumer, executionTarget)
	if err != nil {
		return receipt, nil, err
	}
	if s.lifeGraph == nil {
		return receipt, nil, nil
	}

	var once sync.Once
	var projected Receipt
	return receipt, func(projectionContext context.Context) Receipt {
		once.Do(func() {
			if projectionContext == nil {
				projectionContext = projectionDefaultContext
			}
			if projectionContext == nil {
				projectionContext = context.Background()
			}
			projected = receipt
			s.projectReceipt(projectionContext, &projected)
		})
		return projected
	}, nil
}

func (s *Service) authorizeTaskReviewAndConsume(
	ctx context.Context,
	request Request,
	consumer string,
	executionTarget string,
) (Receipt, error) {
	repository, ok := s.repository.(*PostgresRepository)
	if !ok {
		return Receipt{}, ErrPostgresTransactionRequired
	}
	if resolver, ok := s.approvals.(PostgresTransactionApprovalResolver); !ok || resolver == nil {
		return Receipt{}, ErrPostgresTransactionRequired
	}
	if repository == nil || repository.DB == nil || repository.DB.Statement == nil {
		return Receipt{}, ErrPostgresTransactionRequired
	}
	if _, alreadyTransactional := repository.DB.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return Receipt{}, ErrPostgresTransactionRequired
	}
	db, finish, err := infra.PostgresExecutionDB(ctx, repository.DB)
	if err != nil {
		return Receipt{}, err
	}
	defer finish()
	ctx = db.Statement.Context

	var receipt Receipt
	var project AuthorizationPostCommitProjection
	err = db.Transaction(func(tx *gorm.DB) error {
		var authorizeErr error
		receipt, project, authorizeErr = s.AuthorizeAndConsumeInTransaction(
			ctx,
			tx,
			request,
			consumer,
			executionTarget,
		)
		if errors.Is(authorizeErr, ErrNotAuthorized) &&
			receipt.ID != uuid.Nil && receipt.Outcome != OutcomeAuthorized {
			return nil
		}
		return authorizeErr
	})
	if err != nil {
		return receipt, err
	}
	if receipt.Outcome != OutcomeAuthorized {
		return receipt, ErrNotAuthorized
	}
	if project != nil {
		receipt = project(ctx)
	}
	return receipt, nil
}

func isTaskReviewApprovalSource(sourceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(sourceID), "task-review:")
}

func samePostgresTransaction(database, tx *gorm.DB) bool {
	if database == nil || tx == nil || database.Dialector == nil || tx.Dialector == nil ||
		database.Dialector.Name() != "postgres" || tx.Dialector.Name() != "postgres" ||
		database.Error != nil || tx.Error != nil || database.Statement == nil || tx.Statement == nil {
		return false
	}
	if _, alreadyTransactional := database.Statement.ConnPool.(gorm.TxCommitter); alreadyTransactional {
		return false
	}
	committer, ok := tx.Statement.ConnPool.(gorm.TxCommitter)
	if !ok || committer == nil {
		return false
	}
	committerValue := reflect.ValueOf(committer)
	if (committerValue.Kind() == reflect.Ptr || committerValue.Kind() == reflect.Interface) && committerValue.IsNil() {
		return false
	}

	return infra.PostgresExecutionPoolMatches(database, tx)
}
