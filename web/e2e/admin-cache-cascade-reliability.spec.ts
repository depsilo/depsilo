import type { Request } from '@playwright/test'
import { test, expect, mockAdminApi } from './fixtures/admin-api'

const config = {enabled:true,max_hops:4,max_ttl:'168h',allow_insecure_http:false,token_set:true,peers:[{name:'home',url:'https://cache.example',token_set:true,forward_credentials:false}],config_writable:true,pending_restart:false}
const info = {enabled:true,instance_id:'node-one',relay_path:'/_depsilo/relay/v1',max_hops:4,max_ttl_seconds:604800,allow_insecure_http:false,peers:[{name:'home',url:'https://cache.example',forward_credentials:false}]}

test('failed index filters keep their controls and can be cleared', async ({ page }) => {
  await mockAdminApi(page, {'GET /api/v1/admin/cache/indexes': (request:Request) => new URL(request.url()).searchParams.has('status') ? {status:500,body:{code:'DB_ERROR'}} : {items:[],total:0,summary:[],page:1,page_size:25}})
  await page.goto('/admin/indexes')
  await page.getByRole('combobox',{name:'按有效状态筛选'}).selectOption('stale')
  await expect(page.getByText('无法加载索引缓存信息。')).toBeVisible()
  await expect(page.getByRole('combobox',{name:'按有效状态筛选'})).toHaveValue('stale')
  await page.getByRole('button',{name:'清除筛选'}).click()
  await expect(page.getByText('暂无索引缓存')).toBeVisible()
})

test('cascade refresh synchronizes a clean draft and protects a dirty one', async ({ page }) => {
  let server = {...config}
  let saved: Record<string,unknown> | undefined
  await mockAdminApi(page, {'GET /api/v1/admin/cascade':info,'GET /api/v1/admin/cascade/config':()=>server,'PUT /api/v1/admin/cascade/config':(request:Request)=>{saved=request.postDataJSON();return{state:server,changed:[],restart_required:false}}})
  await page.goto('/admin/cascade')
  await expect(page.getByLabel('最大跳数')).toHaveValue('4')
  await expect(page.locator('[data-cascade-peer-editor]').getByLabel('名称')).toHaveAttribute('readonly','')
  server={...config,max_hops:8}
  await page.getByRole('button',{name:'刷新',exact:true}).click()
  await expect(page.getByLabel('最大跳数')).toHaveValue('8')
  await page.getByLabel('父级缓存上限').fill('48h')
  server={...config,max_hops:12}
  await page.getByRole('button',{name:'刷新',exact:true}).click()
  await expect(page.getByText('配置已被其他操作更新。当前草稿已保留；请重新载入最新配置后再修改。')).toBeVisible()
  await expect(page.getByLabel('父级缓存上限')).toHaveValue('48h')
  await expect(page.getByRole('button',{name:'保存',exact:true})).toBeDisabled()
  expect(saved).toBeUndefined()
  await page.getByRole('button',{name:'放弃草稿并载入最新配置'}).click()
  await expect(page.getByLabel('最大跳数')).toHaveValue('12')
})

test('cascade rechecks server configuration before committing a draft', async ({ page }) => {
  let server={...config}
  let writes=0
  await mockAdminApi(page, {'GET /api/v1/admin/cascade':info,'GET /api/v1/admin/cascade/config':()=>server,'PUT /api/v1/admin/cascade/config':()=>{writes++;return{state:server,changed:[],restart_required:false}}})
  await page.goto('/admin/cascade')
  await page.getByLabel('父级缓存上限').fill('48h')
  server={...config,max_hops:8}
  await page.getByRole('button',{name:'保存',exact:true}).click()
  await expect(page.getByRole('button',{name:'放弃草稿并载入最新配置'})).toBeVisible()
  expect(writes).toBe(0)
})

test('unavailable cascade bindings are unknown rather than zero', async ({ page }) => {
  await mockAdminApi(page, {'GET /api/v1/admin/cascade':info,'GET /api/v1/admin/cascade/config':config,'GET /api/v1/admin/upstreams':{status:500,body:{code:'DB_ERROR'}}})
  await page.goto('/admin/cascade')
  await expect(page.getByText('无法读取上游出口绑定。绑定数量和引用关系暂不可用。')).toBeVisible()
  await expect(page.locator('[data-cascade-topology]')).toContainText('已绑定上游 —')
})

test('readonly snapshot browsing exposes no write controls', async ({ page }) => {
  await mockAdminApi(page, {'GET /api/v1/auth/me':{id:2,username:'reader',role:'readonly',enabled:true,auth_method:'jwt',can_write:false},'GET /api/v1/admin/snapshots':{items:[{id:1,name:'release',note:'',artifact_count:1,total_bytes:12000,created_at:'2026-10-21T00:00:00Z'}],active_snapshot_id:0,active_snapshot:''}})
  await page.goto('/admin/cache?view=snapshots')
  await expect(page.getByRole('region',{name:'快照表格'})).toContainText('release')
  await expect(page.getByRole('button',{name:'导入清单'})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'用于快照模式'})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'删除',exact:true})).toHaveCount(0)
  await expect(page.getByRole('button',{name:'导出',exact:true})).toBeVisible()
})

test('index links restore the package filter and send it to the server', async ({ page }) => {
  let requested = ''
  await mockAdminApi(page, {'GET /api/v1/admin/cache/indexes': (request: Request) => {
    requested = new URL(request.url()).searchParams.get('search') ?? ''
    return {items:[],total:0,summary:[],page:1,page_size:25}
  }})
  await page.goto('/admin/indexes?package=%40types%2Fnode')
  await expect(page.getByRole('textbox', {name:'搜索索引缓存'})).toHaveValue('@types/node')
  await expect.poll(() => requested).toBe('@types/node')
  await page.getByRole('button', {name:'清除筛选'}).click()
  await expect(page).toHaveURL(/\/admin\/indexes$/)
  await expect.poll(() => requested).toBe('')
})
