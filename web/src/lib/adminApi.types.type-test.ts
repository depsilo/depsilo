import { adminApi, authApi } from './api'
import type {
  AccessLogListResponse,
  ApproveSuggestionRequest,
  ApproveSuggestionResponse,
  AuditLogListResponse,
  CreateProjectRequest,
  CreateProjectResponse,
  DeleteProjectResponse,
  DismissSuggestionResponse,
  Principal,
  Project,
  ProjectDetail,
  ProjectListResponse,
  ProjectPackageQuery,
  ProjectPackagesResponse,
  ProjectSBOMQuery,
  RegenerateProjectTokenResponse,
  SecurityDashboard,
  SecurityBaseQuery,
  SecurityImportResponse,
  SecurityPackagePage,
  SecurityPolicy,
  SecurityQuery,
  SecurityScanResponse,
  SecuritySuggestionPage,
  SecurityVulnerabilityPage,
  UpdateProjectRequest,
  UpdateSecurityPolicyRequest,
} from './adminApi.types'

export type Equal<A, B> =
  (<T>() => T extends A ? 1 : 2) extends
  (<T>() => T extends B ? 1 : 2) ? true : false
export type Assert<T extends true> = T
export type ResponseData<T extends (...args: never[]) => unknown> =
  Awaited<ReturnType<T>> extends { data: infer Data } ? Data : never
export type FirstArg<T extends (...args: never[]) => unknown> = Parameters<T>[0]
export type SecondArg<T extends (...args: never[]) => unknown> = Parameters<T>[1]

export type PrincipalContract = Assert<Equal<ResponseData<typeof authApi.me>, Principal>>
export type LogsContract = Assert<Equal<ResponseData<typeof adminApi.listLogs>, AccessLogListResponse>>
export type AuditContract = Assert<Equal<ResponseData<typeof adminApi.listAuditLogs>, AuditLogListResponse>>
export type SecurityDashboardContract = Assert<Equal<ResponseData<typeof adminApi.getSecurityDashboard>, SecurityDashboard>>
export type VulnerabilityContract = Assert<Equal<ResponseData<typeof adminApi.listVulnerabilities>, SecurityVulnerabilityPage>>
export type VulnerablePackagesContract = Assert<Equal<ResponseData<typeof adminApi.listVulnerablePackages>, SecurityPackagePage>>
export type SuggestionsContract = Assert<Equal<ResponseData<typeof adminApi.listSuggestions>, SecuritySuggestionPage>>
export type ApproveSuggestionContract = Assert<Equal<ResponseData<typeof adminApi.approveSuggestion>, ApproveSuggestionResponse>>
export type DismissSuggestionContract = Assert<Equal<ResponseData<typeof adminApi.dismissSuggestion>, DismissSuggestionResponse>>
export type SecurityScanContract = Assert<Equal<ResponseData<typeof adminApi.triggerSecurityScan>, SecurityScanResponse>>
export type SecurityImportContract = Assert<Equal<ResponseData<typeof adminApi.importVulnerabilities>, SecurityImportResponse>>
export type PolicyContract = Assert<Equal<ResponseData<typeof adminApi.listSecurityPolicies>, SecurityPolicy[]>>
export type UpdatePolicyContract = Assert<Equal<ResponseData<typeof adminApi.updateSecurityPolicy>, SecurityPolicy>>
export type SecurityQueryContract = Assert<Equal<FirstArg<typeof adminApi.listVulnerabilities>, SecurityQuery>>
export type VulnerablePackagesQueryContract = Assert<Equal<FirstArg<typeof adminApi.listVulnerablePackages>, SecurityBaseQuery>>
export type SuggestionsQueryContract = Assert<Equal<FirstArg<typeof adminApi.listSuggestions>, SecurityBaseQuery>>
export type ApproveSuggestionInputContract = Assert<Equal<SecondArg<typeof adminApi.approveSuggestion>, ApproveSuggestionRequest | undefined>>
export type SecurityImportInputContract = Assert<Equal<FirstArg<typeof adminApi.importVulnerabilities>, FormData>>
export type UpdatePolicyInputContract = Assert<Equal<SecondArg<typeof adminApi.updateSecurityPolicy>, UpdateSecurityPolicyRequest>>

export type ProjectsContract = Assert<Equal<ResponseData<typeof adminApi.listProjects>, ProjectListResponse>>
export type CreateProjectContract = Assert<Equal<ResponseData<typeof adminApi.createProject>, CreateProjectResponse>>
export type ProjectDetailContract = Assert<Equal<ResponseData<typeof adminApi.getProject>, ProjectDetail>>
export type UpdateProjectContract = Assert<Equal<ResponseData<typeof adminApi.updateProject>, Project>>
export type DeleteProjectContract = Assert<Equal<ResponseData<typeof adminApi.deleteProject>, DeleteProjectResponse>>
export type ProjectPackagesContract = Assert<Equal<ResponseData<typeof adminApi.listProjectPackages>, ProjectPackagesResponse>>
export type RegenerateProjectTokenContract = Assert<Equal<ResponseData<typeof adminApi.regenerateProjectToken>, RegenerateProjectTokenResponse>>
export type ExportProjectSBOMContract = Assert<Equal<ResponseData<typeof adminApi.exportSbom>, Blob>>
export type CreateProjectInputContract = Assert<Equal<FirstArg<typeof adminApi.createProject>, CreateProjectRequest>>
export type UpdateProjectInputContract = Assert<Equal<SecondArg<typeof adminApi.updateProject>, UpdateProjectRequest>>
export type ProjectPackagesInputContract = Assert<Equal<SecondArg<typeof adminApi.listProjectPackages>, ProjectPackageQuery>>
export type ExportProjectSBOMInputContract = Assert<Equal<SecondArg<typeof adminApi.exportSbom>, ProjectSBOMQuery>>
