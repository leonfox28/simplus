import { DeleteOutlined, SendOutlined, UserAddOutlined } from '@ant-design/icons'
import { PageContainer, ProCard, ProForm, ProFormSelect, ProFormText, ProFormTextArea, ProTable } from '@ant-design/pro-components'
import { App, Button, Popconfirm, Space, Tag } from 'antd'
import React, { useCallback, useEffect, useState } from 'react'
import { createContact, deleteContact, deleteSMSMessage, getHardwareTopology, listContacts, listSMSHistory, sendSMSMessage, type Contact, type HardwareTopologyResponse, type SMSMessage } from '@/api/client'

const operationId=()=>crypto.randomUUID?.()??`operation_${Date.now()}_abcdefghijkl`
export default function Messages(){
  const {message}=App.useApp();const [items,setItems]=useState<SMSMessage[]>([]);const [contacts,setContacts]=useState<Contact[]>([]);const [topology,setTopology]=useState<HardwareTopologyResponse>()
  const load=useCallback(async()=>{const [history,c,t]=await Promise.all([listSMSHistory(),listContacts(),getHardwareTopology()]);setItems(history.messages);setContacts(c);setTopology(t)},[])
  useEffect(()=>{void load()},[load])
  return <PageContainer title="短信" subTitle="发送、接收和管理本地短信历史">
    <div className="page-grid two"><ProCard title="发送短信"><ProForm autoFocusFirstInput={false} submitter={{searchConfig:{submitText:'发送'},submitButtonProps:{icon:<SendOutlined/>}}} onFinish={async v=>{await sendSMSMessage({operationId:operationId(),lineId:v.lineId,destination:v.destination,body:v.body});message.success('短信已提交');await load();return true}}>
      <ProFormSelect name="lineId" label="线路" rules={[{required:true}]} options={(topology?.lines??[]).filter(l=>l.state==='ready'&&l.capabilities.sms).map(l=>({value:l.id,label:l.displayName}))}/><ProFormText name="destination" label="号码" rules={[{required:true,pattern:/^\+?[0-9]{3,20}$/}]}/><ProFormTextArea name="body" label="内容" rules={[{required:true}]} fieldProps={{maxLength:1600,showCount:true}}/>
    </ProForm></ProCard><ProCard title="联系人"><ProForm autoFocusFirstInput={false} layout="horizontal" submitter={{searchConfig:{submitText:'添加'},submitButtonProps:{icon:<UserAddOutlined/>}}} onFinish={async v=>{await createContact({displayName:v.displayName,phoneNumber:v.phoneNumber});await load();return true}}><ProFormText name="displayName" label="名称" rules={[{required:true}]}/><ProFormText name="phoneNumber" label="号码" rules={[{required:true}]}/></ProForm><Space wrap>{contacts.map(c=><Tag key={c.id} closable onClose={e=>{e.preventDefault();void deleteContact(c.id).then(load)}}>{c.displayName} · {c.phoneNumber}</Tag>)}</Space></ProCard></div>
    <ProTable style={{marginTop:16}} search={false} rowKey="id" dataSource={items} scroll={{x:'max-content'}} columns={[{title:'时间',dataIndex:'createdAt',valueType:'dateTime'},{title:'线路',dataIndex:'lineId'},{title:'方向',dataIndex:'direction',render:v=><Tag>{String(v)}</Tag>},{title:'号码',dataIndex:'remoteAddress'},{title:'内容',dataIndex:'body',ellipsis:true},{title:'状态',dataIndex:'status',render:v=><Tag color={v==='sent'||v==='received'?'green':v==='failed'?'red':'blue'}>{String(v)}</Tag>},{title:'操作',render:(_,r)=><Popconfirm title="删除这条记录？" onConfirm={async()=>{await deleteSMSMessage(r.id);await load()}}><Button danger type="text" icon={<DeleteOutlined/>}/></Popconfirm>}]} />
  </PageContainer>
}
