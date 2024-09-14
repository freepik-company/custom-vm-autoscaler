package mig

import (
	"context"
	"fmt"
	"math/rand"

	compute "cloud.google.com/go/compute/apiv1"
	computepb "cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"
)

// AddNodeToMIG aumenta el tamaño del Managed Instance Group (MIG) en 1.
func AddNodeToMIG(projectID, zone, migName string) error {
	ctx := context.Background()
	client, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	// Obtener el tamaño actual del MIG
	targetSize, err := getMIGTargetSize(ctx, projectID, zone, migName)
	if err != nil {
		return err
	}

	// Crear la solicitud de redimensionamiento
	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		Size:                 targetSize + 1,
	}

	_, err = client.Resize(ctx, req)
	return err
}

// RemoveNodeFromMIG elimina una instancia aleatoria del Managed Instance Group (MIG) y reduce el tamaño en 1.
func RemoveNodeFromMIG(projectID, zone, migName, instanceToRemove string) error {
	ctx := context.Background()
	migClient, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		return err
	}
	defer migClient.Close()

	// Construir la URL completa de la instancia
	instanceURL := fmt.Sprintf("projects/%s/zones/%s/instances/%s", projectID, zone, instanceToRemove)

	// Construir la solicitud AbandonInstancesInstanceGroupManagerRequest
	abandonReq := &computepb.AbandonInstancesInstanceGroupManagerRequest{
		Project:              projectID,
		Zone:                 zone,
		InstanceGroupManager: migName,
		InstanceGroupManagersAbandonInstancesRequestResource: &computepb.InstanceGroupManagersAbandonInstancesRequest{
			Instances: []string{instanceURL}, // Lista de instancias en formato de URL completo
		},
	}

	// Enviar la solicitud con el cuerpo JSON
	migResp, err := migClient.AbandonInstances(ctx, abandonReq)
	if err != nil {
		fmt.Printf("Error abandoning instance from MIG: %v", err)
	} else {
		fmt.Printf("Instance abandoned from MIG successfully: %v", migResp)

		computeClient, err := compute.NewInstancesRESTClient(ctx)
		if err != nil {
			return err
		}
		defer computeClient.Close()

		// Ahora eliminar la instancia permanentemente de Compute Engine
		instanceDeleteReq := &computepb.DeleteInstanceRequest{
			Project:  projectID,
			Zone:     zone,
			Instance: instanceToRemove, // Nombre de la instancia
		}

		compResp, err := computeClient.Delete(ctx, instanceDeleteReq)
		if err != nil {
			fmt.Printf("Error deleting instance from Compute Engine: %v", err)
		} else {
			fmt.Printf("Instance deleted from Compute Engine successfully: %v", compResp)
		}
	}

	return err
}

// getMIGTargetSize obtiene el valor actual del targetSize de un Managed Instance Group (MIG).
func getMIGTargetSize(ctx context.Context, projectID, zone, migName string) (int32, error) {
	client, err := compute.NewInstanceGroupManagersRESTClient(ctx)
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
	migClient, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		return "", err
	}
	defer migClient.Close()

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
	client, err := compute.NewInstanceGroupManagersRESTClient(ctx)
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
