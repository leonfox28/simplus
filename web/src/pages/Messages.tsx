import { DeleteOutlined, SendOutlined, UserAddOutlined } from '@ant-design/icons'
import { PageContainer, ProCard, ProForm, ProFormSelect, ProFormText, ProFormTextArea, ProTable } from '@ant-design/pro-components'
import { App, Button, Popconfirm, Space, Tag } from 'antd'
import React, { useCallback, useEffect, useState } from 'react'
import { createContact, deleteContact, deleteSMSMessage, listContacts, listManagedLines, listSMSHistory, sendSMSMessage, type Contact, type ManagedLine, type SMSMessage } from '@/api/client'
import { sortSMSMessagesForDisplay } from '@/messages/order'
import { smsStatusPresentation } from '@/messages/status'

const operationId=()=>crypto.randomUUID?.()??`operation_${Date.now()}_abcdefghijkl`
export default function Messages(){
  const {message}=App.useApp();const [items,setItems]=useState<SMSMessage[]>([]);const [contacts,setContacts]=useState<Contact[]>([]);const [lines,setLines]=useState<ManagedLine[]>([])
  const load=useCallback(async()=>{const [history,c,l]=await Promise.all([listSMSHistory(),listContacts(),listManagedLines()]);setItems(sortSMSMessagesForDisplay(history.messages));setContacts(c);setLines(l)},[])
  useEffect(()=>{
    let disposed=false,inFlight=false,errorShown=false
    const refresh=async()=>{
      if(disposed||inFlight)return
      inFlight=true
      try{await load();errorShown=false}catch{if(!disposed&&!errorShown){message.error('短信数据刷新失败');errorShown=true}}finally{inFlight=false}
    }
    void refresh()
    const timer=window.setInterval(()=>{if(document.visibilityState==='visible')void refresh()},5000)
    return()=>{disposed=true;window.clearInterval(timer)}
  },[load,message])
  return <PageContainer title="短信" subTitle="发送、接收和管理本地短信历史">
    <div className="page-grid two"><ProCard title="发送短信"><ProForm autoFocusFirstInput={false} submitter={{searchConfig:{submitText:'发送'},submitButtonProps:{icon:<SendOutlined/>}}} onFinish={async v=>{const result=await sendSMSMessage({operationId:operationId(),lineId:v.lineId,destination:v.destination,body:v.body});await load();if(result.status==='unconfirmed'){message.warning(result.errorCode==='IMS_SMS_ACCEPTED_AWAITING_REPORT'?'短信已提交，正在等待运营商确认，请勿重复发送':'短信可能已经送达，但运营商未返回最终确认，请勿重复发送');return true}if(result.status==='failed'){message.error('短信发送失败');return false}message.success('短信已发送');return true}}>
      <ProFormSelect name="lineId" label="线路" rules={[{required:true}]} options={lines.filter(l=>l.state==='ready'&&(l.capabilities.sms||(l.accessMode==='host-vowifi-only'&&l.capabilities.hostVoWifiAuth))).map(l=>({value:l.id,label:l.displayName}))}/><ProFormText name="destination" label="号码" rules={[{required:true,pattern:/^\+?[0-9]{3,20}$/}]}/><ProFormTextArea name="body" label="内容" rules={[{required:true}]} fieldProps={{maxLength:1600,showCount:true}}/>
    </ProForm></ProCard><ProCard title="联系人"><ProForm autoFocusFirstInput={false} layout="horizontal" submitter={{searchConfig:{submitText:'添加'},submitButtonProps:{icon:<UserAddOutlined/>}}} onFinish={async v=>{await createContact({displayName:v.displayName,phoneNumber:v.phoneNumber});await load();return true}}><ProFormText name="displayName" label="名称" rules={[{required:true}]}/><ProFormText name="phoneNumber" label="号码" rules={[{required:true}]}/></ProForm><Space wrap>{contacts.map(c=><Tag key={c.id} closable onClose={e=>{e.preventDefault();void deleteContact(c.id).then(load)}}>{c.displayName} · {c.phoneNumber}</Tag>)}</Space></ProCard></div>
    <ProTable style={{marginTop:16}} search={false} rowKey="id" dataSource={items} scroll={{x:'max-content'}} columns={[{title:'时间',dataIndex:'createdAt',valueType:'dateTime'},{title:'线路',dataIndex:'lineId'},{title:'方向',dataIndex:'direction',render:v=><Tag>{String(v)}</Tag>},{title:'号码',dataIndex:'remoteAddress'},{title:'内容',dataIndex:'body',ellipsis:true},{title:'状态',dataIndex:'status',render:(_,record)=>{const status=smsStatusPresentation(record);return <Tag color={status.color}>{status.label}</Tag>}},{title:'操作',render:(_,r)=><Popconfirm title="删除这条记录？" onConfirm={async()=>{await deleteSMSMessage(r.id);await load()}}><Button danger type="text" icon={<DeleteOutlined/>}/></Popconfirm>}]} />
  </PageContainer>
}
