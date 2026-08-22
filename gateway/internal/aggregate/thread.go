package aggregate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/apitypes"
)

// errUpstreamStatus says a service answered, but not with the 200 the read
// expected. It is static so a caller can tell it apart from a transport error.
var errUpstreamStatus = errors.New("upstream answered with an unexpected status")

// Thread reads the skeleton's one end to end thread: the tutor from identity,
// which owns it, and from notifications how far its own projection has caught
// up. The two calls go out at the same time, so a phone first screen pays one
// round trip rather than two (spec 0001).
//
// The tutor panel is required: without it there is nothing to show. The
// projection panel degrades on its own, because one slow service should only
// take out its own panel.
func (c *Client) Thread(ctx context.Context, tutorID uuid.UUID, bearer string) (apitypes.ThreadStatus, error) {
	var status apitypes.ThreadStatus

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		response, err := c.Call(groupCtx, http.MethodGet, c.upstreams.Identity, "/me", bearer, nil)
		if err != nil {
			return err
		}
		if response.Status != http.StatusOK {
			return fmt.Errorf("identity answered %d: %w", response.Status, errUpstreamStatus)
		}
		return json.Unmarshal(response.Body, &status.Tutor)
	})

	var projectionErr error
	group.Go(func() error {
		projectionErr = c.readProjection(groupCtx, tutorID, bearer, &status.Projection)
		// The projection panel degrades on its own, so its failure is carried
		// out of the group rather than failing it.
		return nil
	})

	err := group.Wait()
	if err != nil {
		return apitypes.ThreadStatus{}, err
	}
	if projectionErr != nil {
		message := projectionErr.Error()
		status.ProjectionUnavailable = &message
		status.Projection = apitypes.ProjectionStatus{TutorId: tutorID}
	}
	return status, nil
}

// readProjection reads how far notifications has caught up. Its error is
// returned rather than raised, because the caller turns it into an unavailable
// panel instead of a failed screen.
func (c *Client) readProjection(ctx context.Context, tutorID uuid.UUID, bearer string, into *apitypes.ProjectionStatus) error {
	response, err := c.Call(ctx, http.MethodGet, c.upstreams.Notifications,
		"/recipients/"+tutorID.String()+"/status", bearer, nil)
	if err != nil {
		return err
	}
	if response.Status != http.StatusOK {
		return fmt.Errorf("notifications answered %d: %w", response.Status, errUpstreamStatus)
	}
	return json.Unmarshal(response.Body, into)
}
