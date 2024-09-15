package google

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	"elasticsearch-vm-autoscaler/internal/elasticsearch"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// AddNodeToMIG aumenta el tamaño del Managed Instance Group (MIG) en 1, si no ha alcanzado el máximo.
func AddNodeToMIG(projectID, zone, migName string, debugMode bool) error {

	ctx := context.Background()
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return err
	}
	defer client.Close()

	// Obtener el tamaño actual del MIG
	targetSize, err := getMIGTargetSize(ctx, projectID, zone, migName)
	if err != nil {
		return err
	}

	// Obtener los límites de escalado (mínimo y máximo)
	_, maxSize, err := getMIGScalingLimits(ctx, projectID, zone, migName)
	if err != nil {
		return err
	}

	// Verificar si el tamaño actual está en los límites permitidos
	if targetSize >= maxSize {
		fmt.Printf("El tamaño del MIG ya ha alcanzado el máximo (%d), no se puede aumentar más.\n", maxSize)
		return nil
	}

	// Crear la solicitud de redimensionamiento
	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		Size:                 targetSize + 1,
	}

	if !debugMode {
		_, err = client.Resize(ctx, req)
		return err
	}
	return nil
}

// RemoveNodeFromMIG reduce el tamaño del Managed Instance Group (MIG) en 1, si no ha alcanzado el mínimo.
func RemoveNodeFromMIG(projectID, zone, migName, elasticURL, elasticUser, elasticPassword string, debugMode bool) error {

	ctx := context.Background()
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return err
	}
	defer client.Close()

	// Obtener el tamaño actual del MIG
	targetSize, err := getMIGTargetSize(ctx, projectID, zone, migName)
	if err != nil {
		return err
	}

	// Obtener los límites de escalado (mínimo y máximo)
	minSize, _, err := getMIGScalingLimits(ctx, projectID, zone, migName)
	if err != nil {
		return err
	}

	// Verificar si el tamaño actual está en los límites permitidos
	if targetSize <= minSize {
		fmt.Printf("El tamaño del MIG ya ha alcanzado el mínimo (%d), no se puede reducir más.\n", minSize)
		return nil
	}

	instanceToRemove, err := GetInstanceToRemove(projectID, zone, migName)
	if err != nil {
		log.Printf("Error getting instance to remove: %v", err)
		return err
	}

	if !debugMode {
		err = elasticsearch.DrainElasticsearchNode(elasticURL, instanceToRemove, elasticUser, elasticPassword)
		if err != nil {
			log.Printf("Error draining node in Elasticsearch: %v", err)
			return err
		}
	}

	// Abandonar una instancia aleatoria y reducir el tamaño
	instanceURL := fmt.Sprintf("projects/%s/zones/%s/instances/%s", projectID, zone, instanceToRemove)
	abandonReq := &computepb.AbandonInstancesInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		InstanceGroupManagersAbandonInstancesRequestResource: &computepb.InstanceGroupManagersAbandonInstancesRequest{
			Instances: []string{instanceURL},
		},
	}

	if !debugMode {
		_, err = client.AbandonInstances(ctx, abandonReq)
		return err
	}
	return nil
}

// getMIGScalingLimits obtiene los límites de escalado (mínimo y máximo) de un Managed Instance Group (MIG).
func getMIGScalingLimits(ctx context.Context, projectID, zone, migName string) (int32, int32, error) {
	client, err := createComputeClient(ctx, compute.NewAutoscalersRESTClient)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create Autoscaler client: %v", err)
	}
	defer client.Close()

	// Crear la solicitud para obtener el autoscaler asociado al MIG
	req := &computepb.GetAutoscalerRequest{
		Project:    projectID,
		Zone:       zone,
		Autoscaler: fmt.Sprintf("%s-autoscaler", migName), // Se asume que el nombre del autoscaler es el nombre del MIG con "-autoscaler"
	}

	autoscaler, err := client.Get(ctx, req)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get autoscaler: %v", err)
	}

	// Obtener los límites del autoscaler
	minSize := autoscaler.AutoscalingPolicy.MinNumReplicas
	maxSize := autoscaler.AutoscalingPolicy.MaxNumReplicas

	return *minSize, *maxSize, nil
}

// getMIGTargetSize obtiene el valor actual del targetSize de un Managed Instance Group (MIG).
func getMIGTargetSize(ctx context.Context, projectID, zone, migName string) (int32, error) {
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return 0, fmt.Errorf("failed to create Compute Engine client: %v", err)
	}
	defer client.Close()

	// Obtener el Managed Instance Group (MIG)
	req := &computepb.GetInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
	}

	mig, err := client.Get(ctx, req)
	if err != nil {
		return 0, fmt.Errorf("failed to get MIG: %v", err)
	}

	// Retornar el tamaño actual del MIG (targetSize)
	return mig.GetTargetSize(), nil
}

func GetInstanceToRemove(projectID, zone, migName string) (string, error) {
	ctx := context.Background()
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return "", err
	}
	defer client.Close()

	// Obtener la lista de instancias en el MIG
	instanceNames, err := getMIGInstanceNames(ctx, projectID, zone, migName)
	if err != nil {
		return "", err
	}
	if len(instanceNames) == 0 {
		return "", fmt.Errorf("no instances found in the MIG")
	}

	// Seleccionar una instancia aleatoria
	return instanceNames[rand.Intn(len(instanceNames))], nil
}

// getMIGInstanceNames obtiene la lista de nombres de instancias en un Managed Instance Group (MIG).
func getMIGInstanceNames(ctx context.Context, projectID, zone, migName string) ([]string, error) {
	// Crear cliente para la API de InstanceGroupManagers
	client, err := createComputeClient(ctx, compute.NewInstanceGroupManagersRESTClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create Compute Engine client: %v", err)
	}
	defer client.Close()

	// Crear la solicitud para listar instancias manejadas por el MIG
	req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
	}

	// Llamar a la API para listar instancias (devuelve un iterador)
	it := client.ListManagedInstances(ctx, req)

	// Variable para almacenar los nombres de las instancias
	var instanceNames []string

	// Recorrer el iterador para obtener las instancias
	for {
		instance, err := it.Next()
		if err == iterator.Done {
			break // Cuando el iterador ha terminado
		}
		if err != nil {
			return nil, fmt.Errorf("failed to list managed instances: %v", err)
		}

		// Agregar el nombre de la instancia a la lista
		instanceNames = append(instanceNames, *instance.Instance)
	}

	return instanceNames, nil
}
