package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nextcloudv1 "github.com/LoriKarikari/nextcloud-operator/api/v1"
)

type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *Reconciler) ReconcileDatabase(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling PostgreSQL database", "name", nc.Name, "namespace", nc.Namespace)

	if err := r.reconcileDatabaseSecret(ctx, nc); err != nil {
		return err
	}

	if err := r.reconcileDatabaseStatefulSet(ctx, nc); err != nil {
		return err
	}

	if err := r.reconcileDatabaseService(ctx, nc); err != nil {
		return err
	}

	return nil
}

func (r *Reconciler) generatePassword() string {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-password-" + string(bytes[:8])
	}
	return base64.URLEncoding.EncodeToString(bytes)[:32]
}

func (r *Reconciler) reconcileDatabaseSecret(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	secretName := nc.Name + "-postgres"
	secret := &corev1.Secret{}

	err := r.Get(ctx, client.ObjectKey{Name: secretName, Namespace: nc.Namespace}, secret)
	if err != nil && errors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: nc.Namespace,
			},
			StringData: map[string]string{
				"POSTGRES_DB":       "nextcloud",
				"POSTGRES_USER":     "nextcloud",
				"POSTGRES_PASSWORD": r.generatePassword(),
			},
		}
		ctrl.SetControllerReference(nc, secret, r.Scheme)
		return r.Create(ctx, secret)
	}
	return err
}

func (r *Reconciler) reconcileDatabaseStatefulSet(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	statefulSet := r.statefulSetForPostgreSQL(nc)

	found := &appsv1.StatefulSet{}
	err := r.Get(ctx, client.ObjectKey{Name: statefulSet.Name, Namespace: statefulSet.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, statefulSet)
	} else if err != nil {
		return err
	}

	if found.Spec.Template.Spec.Containers[0].Image != statefulSet.Spec.Template.Spec.Containers[0].Image {
		found.Spec.Template = statefulSet.Spec.Template
		return r.Update(ctx, found)
	}
	return nil
}

func (r *Reconciler) reconcileDatabaseService(ctx context.Context, nc *nextcloudv1.Nextcloud) error {
	service := r.serviceForPostgreSQL(nc)

	found := &corev1.Service{}
	err := r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		return r.Create(ctx, service)
	} else if err != nil {
		return err
	}

	found.Spec.Selector = service.Spec.Selector
	found.Spec.Ports = service.Spec.Ports
	return r.Update(ctx, found)
}

func (r *Reconciler) statefulSetForPostgreSQL(nc *nextcloudv1.Nextcloud) *appsv1.StatefulSet {
	labels := map[string]string{
		"app":       "postgres",
		"component": "database",
		"instance":  nc.Name,
	}

	postgresVersion := "16"
	if nc.Spec.Database.Version != "" {
		postgresVersion = nc.Spec.Database.Version
	}

	storageSize := "8Gi"
	if nc.Spec.Database.StorageSize != "" {
		storageSize = nc.Spec.Database.StorageSize
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-postgres",
			Namespace: nc.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: nc.Name + "-postgres",
			Replicas:    &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "postgres",
						Image: "postgres:" + postgresVersion,
						Ports: []corev1.ContainerPort{{
							ContainerPort: 5432,
							Name:          "postgres",
						}},
						EnvFrom: []corev1.EnvFromSource{{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: nc.Name + "-postgres",
								},
							},
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "postgres-storage",
							MountPath: "/var/lib/postgresql/data",
						}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"pg_isready",
										"-U", "nextcloud",
										"-d", "nextcloud",
									},
								},
							},
							InitialDelaySeconds: 5,
							PeriodSeconds:       5,
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{
									Command: []string{
										"pg_isready",
										"-U", "nextcloud",
										"-d", "nextcloud",
									},
								},
							},
							InitialDelaySeconds: 30,
							PeriodSeconds:       10,
						},
					}},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "postgres-storage",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse(storageSize),
						},
					},
				},
			}},
		},
	}

	if nc.Spec.Database.StorageClass != nil {
		statefulSet.Spec.VolumeClaimTemplates[0].Spec.StorageClassName = nc.Spec.Database.StorageClass
	}

	ctrl.SetControllerReference(nc, statefulSet, r.Scheme)
	return statefulSet
}

func (r *Reconciler) serviceForPostgreSQL(nc *nextcloudv1.Nextcloud) *corev1.Service {
	labels := map[string]string{
		"app":       "postgres",
		"component": "database",
		"instance":  nc.Name,
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nc.Name + "-postgres",
			Namespace: nc.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{{
				Port:       5432,
				TargetPort: intstr.FromInt(5432),
				Name:       "postgres",
			}},
			ClusterIP: "None",
		},
	}

	ctrl.SetControllerReference(nc, service, r.Scheme)
	return service
}
