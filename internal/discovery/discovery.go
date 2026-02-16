package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	eksTypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/phenixrizen/rift/internal/config"
	"golang.org/x/sync/errgroup"
)

type RoleAccess struct {
	AccountID   string
	AccountName string
	RoleName    string
}

type ClusterAccess struct {
	AccountID                string
	AccountName              string
	RoleName                 string
	Region                   string
	ClusterName              string
	ClusterARN               string
	ClusterEndpoint          string
	ClusterCertificateBase64 string
}

type Inventory struct {
	GeneratedAt time.Time
	Roles       []RoleAccess
	Clusters    []ClusterAccess
}

func Discover(ctx context.Context, cfg config.Config, logger *slog.Logger) (Inventory, error) {
	now := time.Now().UTC()
	token, err := loadTokenFromCache(cfg.SSOStartURL, cfg.SSORegion, now)
	if err != nil {
		return Inventory{}, err
	}

	ssoClient := sso.New(sso.Options{Region: cfg.SSORegion})
	accounts, err := listAccounts(ctx, ssoClient, token.AccessToken)
	if err != nil {
		return Inventory{}, fmt.Errorf("list accounts: %w", err)
	}

	roles, err := listRoles(ctx, ssoClient, token.AccessToken, accounts, logger)
	if err != nil {
		return Inventory{}, fmt.Errorf("list account roles: %w", err)
	}

	inv := Inventory{
		GeneratedAt: now,
		Roles:       roles,
	}

	clusters, err := listAllClusters(ctx, ssoClient, token.AccessToken, cfg.Regions, roles, logger)
	if err != nil {
		return Inventory{}, fmt.Errorf("list clusters: %w", err)
	}
	inv.Clusters = clusters

	sort.Slice(inv.Roles, func(i, j int) bool {
		left := inv.Roles[i].AccountName + "|" + inv.Roles[i].RoleName
		right := inv.Roles[j].AccountName + "|" + inv.Roles[j].RoleName
		return left < right
	})
	sort.Slice(inv.Clusters, func(i, j int) bool {
		left := inv.Clusters[i].AccountName + "|" + inv.Clusters[i].RoleName + "|" + inv.Clusters[i].Region + "|" + inv.Clusters[i].ClusterName
		right := inv.Clusters[j].AccountName + "|" + inv.Clusters[j].RoleName + "|" + inv.Clusters[j].Region + "|" + inv.Clusters[j].ClusterName
		return left < right
	})

	return inv, nil
}

func ValidateSSOLogin(cfg config.Config, now time.Time) error {
	_, err := loadTokenFromCache(cfg.SSOStartURL, cfg.SSORegion, now)
	return err
}

type account struct {
	ID   string
	Name string
}

func listAccounts(ctx context.Context, client *sso.Client, accessToken string) ([]account, error) {
	accounts := make([]account, 0)
	input := &sso.ListAccountsInput{AccessToken: aws.String(accessToken)}
	for {
		out, err := client.ListAccounts(ctx, input)
		if err != nil {
			return nil, err
		}
		for _, acct := range out.AccountList {
			accounts = append(accounts, account{
				ID:   aws.ToString(acct.AccountId),
				Name: aws.ToString(acct.AccountName),
			})
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		input.NextToken = out.NextToken
	}
	return accounts, nil
}

func listRoles(ctx context.Context, client *sso.Client, accessToken string, accounts []account, logger *slog.Logger) ([]RoleAccess, error) {
	roles := make([]RoleAccess, 0)
	for _, acct := range accounts {
		input := &sso.ListAccountRolesInput{
			AccessToken: aws.String(accessToken),
			AccountId:   aws.String(acct.ID),
		}
		for {
			out, err := client.ListAccountRoles(ctx, input)
			if err != nil {
				if logger != nil {
					logger.Warn("unable to list account roles", "account_id", acct.ID, "account", acct.Name, "error", err)
				}
				break
			}
			for _, role := range out.RoleList {
				roles = append(roles, RoleAccess{
					AccountID:   acct.ID,
					AccountName: acct.Name,
					RoleName:    aws.ToString(role.RoleName),
				})
			}
			if out.NextToken == nil || *out.NextToken == "" {
				break
			}
			input.NextToken = out.NextToken
		}
	}
	return roles, nil
}

func listAllClusters(
	ctx context.Context,
	ssoClient *sso.Client,
	accessToken string,
	regions []string,
	roles []RoleAccess,
	logger *slog.Logger,
) ([]ClusterAccess, error) {
	if len(roles) == 0 {
		return nil, nil
	}

	var (
		mu       sync.Mutex
		clusters []ClusterAccess
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for _, role := range roles {
		role := role
		g.Go(func() error {
			creds, err := getRoleCredentials(ctx, ssoClient, accessToken, role.AccountID, role.RoleName)
			if err != nil {
				if logger != nil {
					logger.Warn("unable to get role credentials", "account_id", role.AccountID, "account", role.AccountName, "role", role.RoleName, "error", err)
				}
				return nil
			}

			roleClusters := make([]ClusterAccess, 0)
			for _, region := range regions {
				found, err := listClustersForRegion(ctx, region, role, creds)
				if err != nil {
					if logger != nil {
						logger.Warn("unable to list clusters", "account_id", role.AccountID, "account", role.AccountName, "role", role.RoleName, "region", region, "error", err)
					}
					continue
				}
				roleClusters = append(roleClusters, found...)
			}

			mu.Lock()
			clusters = append(clusters, roleClusters...)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return clusters, nil
}

func getRoleCredentials(ctx context.Context, client *sso.Client, accessToken, accountID, roleName string) (aws.CredentialsProvider, error) {
	out, err := client.GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(accessToken),
		AccountId:   aws.String(accountID),
		RoleName:    aws.String(roleName),
	})
	if err != nil {
		return nil, err
	}
	if out.RoleCredentials == nil {
		return nil, fmt.Errorf("empty role credentials")
	}
	provider := credentials.NewStaticCredentialsProvider(
		aws.ToString(out.RoleCredentials.AccessKeyId),
		aws.ToString(out.RoleCredentials.SecretAccessKey),
		aws.ToString(out.RoleCredentials.SessionToken),
	)
	return provider, nil
}

func listClustersForRegion(ctx context.Context, region string, role RoleAccess, provider aws.CredentialsProvider) ([]ClusterAccess, error) {
	cfg := aws.Config{
		Region:      region,
		Credentials: aws.NewCredentialsCache(provider),
	}
	eksClient := eks.NewFromConfig(cfg)

	names := make([]string, 0)
	input := &eks.ListClustersInput{}
	for {
		out, err := eksClient.ListClusters(ctx, input)
		if err != nil {
			return nil, err
		}
		names = append(names, out.Clusters...)
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		input.NextToken = out.NextToken
	}

	clusters := make([]ClusterAccess, 0, len(names))
	for _, name := range names {
		desc, err := eksClient.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
		if err != nil {
			continue
		}
		record := buildClusterRecord(role, region, desc.Cluster)
		if record.ClusterName == "" {
			record.ClusterName = name
		}
		if record.ClusterName == "" {
			continue
		}
		clusters = append(clusters, record)
	}
	return clusters, nil
}

func buildClusterRecord(role RoleAccess, region string, cluster *eksTypes.Cluster) ClusterAccess {
	var arn, endpoint, certData, clusterName string
	if cluster != nil {
		arn = aws.ToString(cluster.Arn)
		endpoint = aws.ToString(cluster.Endpoint)
		clusterName = aws.ToString(cluster.Name)
		if cluster.CertificateAuthority != nil {
			certData = aws.ToString(cluster.CertificateAuthority.Data)
		}
	}
	return ClusterAccess{
		AccountID:                role.AccountID,
		AccountName:              role.AccountName,
		RoleName:                 role.RoleName,
		Region:                   region,
		ClusterName:              clusterName,
		ClusterARN:               arn,
		ClusterEndpoint:          endpoint,
		ClusterCertificateBase64: certData,
	}
}
