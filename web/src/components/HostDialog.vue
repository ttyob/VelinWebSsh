<script setup lang="ts">
import { reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, json } from '../api'
import type { Credential, Host } from '../types'

const props = defineProps<{ modelValue: boolean; host?: Host; credentials: Credential[] }>()
const emit = defineEmits<{ 'update:modelValue':[value:boolean]; saved:[host:Host] }>()
const form = reactive<Host>({ id:'', name:'', address:'', port:22, username:'root', credentialID:'', groupName:'', tags:'', notes:'', favorite:false })
watch(() => props.modelValue, open => { if (open) Object.assign(form, props.host || { id:'', name:'', address:'', port:22, username:'root', credentialID:'', groupName:'', tags:'', notes:'', favorite:false }) })
async function save(){try{const method=form.id?'PUT':'POST';const path=form.id?`/api/hosts/${form.id}`:'/api/hosts';const saved=await api<Host>(path,{method,body:json(form)});emit('saved',saved);emit('update:modelValue',false);ElMessage.success('主机已保存')}catch(e){ElMessage.error(e instanceof Error?e.message:'保存失败')}}
</script>
<template><el-dialog :model-value="modelValue" :title="form.id?'编辑主机':'新增主机'" width="min(560px, 94vw)" @update:model-value="emit('update:modelValue',$event)"><el-form label-position="top"><div class="form-grid"><el-form-item label="名称"><el-input v-model="form.name" /></el-form-item><el-form-item label="分组"><el-input v-model="form.groupName" placeholder="生产环境" /></el-form-item><el-form-item label="主机地址" class="span-2"><el-input v-model="form.address" placeholder="server.example.com" /></el-form-item><el-form-item label="端口"><el-input-number v-model="form.port" :min="1" :max="65535" controls-position="right" /></el-form-item><el-form-item label="用户名"><el-input v-model="form.username" /></el-form-item><el-form-item label="凭据" class="span-2"><el-select v-model="form.credentialID" clearable placeholder="连接时输入"><el-option v-for="c in credentials" :key="c.id" :label="c.name" :value="c.id" /></el-select></el-form-item><el-form-item label="标签" class="span-2"><el-input v-model="form.tags" placeholder="逗号分隔" /></el-form-item><el-form-item label="备注" class="span-2"><el-input v-model="form.notes" type="textarea" :rows="2" /></el-form-item></div></el-form><template #footer><el-button @click="emit('update:modelValue',false)">取消</el-button><el-button type="primary" @click="save">保存</el-button></template></el-dialog></template>
