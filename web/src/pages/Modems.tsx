import { PageContainer, ProCard, ProDescriptions, ProTable } from '@ant-design/pro-components'
import { Alert, Button, Space, Tag } from 'antd'
import React, { useCallback, useEffect, useState } from 'react'
import { activateEUICCProfile, getEUICCState, getHardwareTopology, type EUICCState, type HardwareTopologyResponse } from '@/api/client'

export default function Modems() {
  const [data, setData] = useState<HardwareTopologyResponse>()
  const [euicc, setEUICC] = useState<EUICCState>()
  const [error, setError] = useState('')
  const load = useCallback(async () => { try { setData(await getHardwareTopology()); try { setEUICC(await getEUICCState()) } catch {} } catch(e) { setError(String(e)) } }, [])
  useEffect(() => { void load() }, [load])
  return <PageContainer title="模组配置" subTitle="查看只读硬件身份、SIM 介质和已验证能力" extra={<Button onClick={load}>刷新</Button>}>
    {error && <Alert type="error" message={error} style={{ marginBottom: 16 }} />}
    <ProTable search={false} options={false} rowKey="id" loading={!data} dataSource={data?.devices ?? []} scroll={{x:'max-content'}} columns={[
      { title:'设备', dataIndex:'displayName' }, { title:'ID', dataIndex:'id', render:v=><span className="mono">{String(v)}</span> },
      { title:'传输', dataIndex:'transport' }, { title:'状态', dataIndex:'state', render:v=><Tag color={v==='available'?'green':'orange'}>{String(v)}</Tag> },
    ]} />
    <ProCard title="Modem Functions" style={{ marginTop:16 }}><ProTable search={false} options={false} pagination={false} rowKey="id" dataSource={data?.modemFunctions ?? []} scroll={{x:'max-content'}} columns={[
      { title:'名称', dataIndex:'displayName' }, { title:'后端', dataIndex:'backend' }, { title:'ID', dataIndex:'id', render:v=><span className="mono">{String(v)}</span> },
      { title:'能力', dataIndex:'capabilities', render:v=><Space wrap>{Object.entries((v??{}) as Record<string,boolean>).filter(([,x])=>x).map(([k])=><Tag key={k}>{k}</Tag>)}</Space> },
    ]} /></ProCard>
    {euicc && <ProCard title={`可拔插 eUICC · ${euicc.eidHint}`} style={{ marginTop:16 }}><Space wrap style={{width:'100%'}}>{euicc.profiles.map(p=><ProCard key={p.id} variant="outlined" style={{ width:'100%', maxWidth:280 }}><ProDescriptions column={1} dataSource={p} columns={[{title:'Profile',dataIndex:'displayName'},{title:'Identity',dataIndex:'displayIdentityHint'}]} /><Button type={p.active?'primary':'default'} disabled={p.active} onClick={async()=>{setEUICC(await activateEUICCProfile(p.id))}}>{p.active?'当前 Profile':'激活'}</Button></ProCard>)}</Space></ProCard>}
  </PageContainer>
}
