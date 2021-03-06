package sous

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/satori/go.uuid"
)

/*
The imagined use case here is like this:

intendedSet := getFromManifests()
existingSet := getFromSingularity()

dChans := intendedSet.Diff(existingSet)

Rectify(dChans)
*/

type (
	rectifier struct {
		sing RectificationClient
	}

	// RectificationClient abstracts the raw interactions with Singularity.
	// The methods on this interface are tightly bound to the semantics of Singularity itself -
	// it's recommended to interact with the Sous Recify function or the recitification driver
	// rather than with implentations of this interface directly.
	RectificationClient interface {
		// Deploy creates a new deploy on a particular requeust
		Deploy(cluster, depID, reqID, dockerImage string, r Resources, e Env, vols Volumes) error

		// PostRequest sends a request to a Singularity cluster to initiate
		PostRequest(cluster, reqID string, instanceCount int) error

		// Scale updates the instanceCount associated with a request
		Scale(cluster, reqID string, instanceCount int, message string) error

		// DeleteRequest instructs Singularity to delete a particular request
		DeleteRequest(cluster, reqID, message string) error

		//ImageName finds or guesses a docker image name for a Deployment
		ImageName(d *Deployment) (string, error)

		//ImageLabels finds the (sous) docker labels for a given image name
		ImageLabels(imageName string) (labels map[string]string, err error)
	}

	dtoMap map[string]interface{}

	// CreateError is returned when there's an error trying to create a deployment
	CreateError struct {
		Deployment *Deployment
		Err        error
	}

	// DeleteError is returned when there's an error while trying to delete a deployment
	DeleteError struct {
		Deployment *Deployment
		Err        error
	}

	// ChangeError describes an error that occurred while trying to change one deployment into another
	ChangeError struct {
		Deployments *DeploymentPair
		Err         error
	}

	// RectificationError is an interface that extends error with methods to get
	// the deployments the preceeded and were intended when the error occurred
	RectificationError interface {
		error
		ExistingDeployment() *Deployment
		IntendedDeployment() *Deployment
	}
)

func (e *CreateError) Error() string {
	return fmt.Sprintf("Couldn't create deployment %+v: %v", e.Deployment, e.Err)
}

// ExistingDeployment returns the deployment that was already existent in a change error
func (e *CreateError) ExistingDeployment() *Deployment {
	return nil
}

// IntendedDeployment returns the deployment that was intended in a ChangeError
func (e *CreateError) IntendedDeployment() *Deployment {
	return e.Deployment
}

func (e *DeleteError) Error() string {
	return fmt.Sprintf("Couldn't delete deployment %+v: %v", e.Deployment, e.Err)
}

// ExistingDeployment returns the deployment that was already existent in a change error
func (e *DeleteError) ExistingDeployment() *Deployment {
	return e.Deployment
}

// IntendedDeployment returns the deployment that was intended in a ChangeError
func (e *DeleteError) IntendedDeployment() *Deployment {
	return nil
}

func (e *ChangeError) Error() string {
	return fmt.Sprintf("Couldn't change from deployment %+v to deployment %+v: %v", e.Deployments.prior, e.Deployments.post, e.Err)
}

// ExistingDeployment returns the deployment that was already existent in a change error
func (e *ChangeError) ExistingDeployment() *Deployment {
	return e.Deployments.prior
}

// IntendedDeployment returns the deployment that was intended in a ChangeError
func (e *ChangeError) IntendedDeployment() *Deployment {
	return e.Deployments.post
}

// Rectify takes a DiffChans and issues the commands to the infrastructure to reconcile the differences
func Rectify(dcs DiffChans, s RectificationClient) chan RectificationError {
	errs := make(chan RectificationError)
	rect := rectifier{s}
	wg := &sync.WaitGroup{}
	wg.Add(3)
	go func() { rect.rectifyCreates(dcs.Created, errs); wg.Done() }()
	go func() { rect.rectifyDeletes(dcs.Deleted, errs); wg.Done() }()
	go func() { rect.rectifyModifys(dcs.Modified, errs); wg.Done() }()
	go func() { wg.Wait(); close(errs) }()

	return errs
}

func (r *rectifier) rectifyCreates(cc chan *Deployment, errs chan<- RectificationError) {
	for d := range cc {
		name, err := r.sing.ImageName(d)
		if err != nil {
			// log.Printf("% +v", d)
			errs <- &CreateError{Deployment: d, Err: err}
			continue
		}

		reqID := computeRequestID(d)
		err = r.sing.PostRequest(d.Cluster, reqID, d.NumInstances)
		if err != nil {
			// log.Printf("%T %#v", d, d)
			errs <- &CreateError{Deployment: d, Err: err}
			continue
		}

		err = r.sing.Deploy(d.Cluster, newDepID(), reqID, name, d.Resources, d.Env, d.DeployConfig.Volumes)
		if err != nil {
			// log.Printf("% +v", d)
			errs <- &CreateError{Deployment: d, Err: err}
			continue
		}
	}
}

func (r *rectifier) rectifyDeletes(dc chan *Deployment, errs chan<- RectificationError) {
	for d := range dc {
		err := r.sing.DeleteRequest(d.Cluster, computeRequestID(d), "deleting request for removed manifest")
		if err != nil {
			errs <- &DeleteError{Deployment: d, Err: err}
			continue
		}
	}
}

func (r *rectifier) rectifyModifys(
	mc chan *DeploymentPair, errs chan<- RectificationError) {
	for pair := range mc {
		Log.Debug.Printf("Rectifying modify: \n  %+ v \n    =>  \n  %+ v", pair.prior, pair.post)
		if r.changesReq(pair) {
			Log.Debug.Printf("Scaling...")
			err := r.sing.Scale(
				pair.post.Cluster,
				computeRequestID(pair.post),
				pair.post.NumInstances,
				"rectified scaling")
			if err != nil {
				errs <- &ChangeError{Deployments: pair, Err: err}
				continue
			}
		}

		if changesDep(pair) {
			Log.Debug.Printf("Deploying...")
			name, err := r.sing.ImageName(pair.post)
			if err != nil {
				errs <- &ChangeError{Deployments: pair, Err: err}
				continue
			}

			err = r.sing.Deploy(
				pair.post.Cluster,
				newDepID(),
				computeRequestID(pair.prior),
				name,
				pair.post.Resources,
				pair.post.Env,
				pair.post.DeployConfig.Volumes,
			)
			if err != nil {
				errs <- &ChangeError{Deployments: pair, Err: err}
				continue
			}
		}
	}
}

func (r rectifier) changesReq(pair *DeploymentPair) bool {
	return pair.prior.NumInstances != pair.post.NumInstances
}

func changesDep(pair *DeploymentPair) bool {
	return !(pair.prior.SourceVersion.Equal(pair.post.SourceVersion) &&
		pair.prior.Resources.Equal(pair.post.Resources) &&
		pair.prior.Env.Equal(pair.post.Env))
}

func computeRequestID(d *Deployment) string {
	if len(d.RequestID) > 0 {
		return d.RequestID
	}
	return idify(d.SourceVersion.CanonicalName().String())
}

var notInIDRE = regexp.MustCompile(`[-/:]`)

func idify(in string) string {
	return notInIDRE.ReplaceAllString(in, "")
}

func newDepID() string {
	return idify(uuid.NewV4().String())
}
