import React, { useState, useEffect } from 'react';
import { Modal, Button, Alert, Typography, Space, Tag, message } from 'antd';
import { ExclamationCircleOutlined, CheckCircleOutlined, DatabaseOutlined } from '@ant-design/icons';

const { Title, Paragraph, Text } = Typography;

interface SQLConfirmModalProps {
  visible: boolean;
  sql: string;
  operationType: 'SELECT' | 'INSERT' | 'UPDATE' | 'DELETE' | 'DDL';
  riskLevel: 'low' | 'medium' | 'high';
  description?: string;
  affectedRows?: number;
  tableName?: string;
  onConfirm: (confirmedSql: string) => void;
  onCancel: () => void;
}

/**
 * SQL 确认弹窗组件
 * 
 * 功能：
 * 1. 显示 AI 生成的 SQL
 * 2. 显示操作类型和风险等级
 * 3. 用户确认后添加确认标记
 * 4. 调用后端执行接口
 */
const SQLConfirmModal: React.FC<SQLConfirmModalProps> = ({
  visible,
  sql,
  operationType,
  riskLevel,
  description,
  affectedRows,
  tableName,
  onConfirm,
  onCancel,
}) => {
  const [confirmLoading, setConfirmLoading] = useState(false);

  // 获取当前用户信息（从 localStorage 或全局状态）
  const getCurrentUser = () => {
    const userStr = localStorage.getItem('userInfo');
    if (userStr) {
      const user = JSON.parse(userStr);
      return user.username || user.name || 'anonymous';
    }
    return 'anonymous';
  };

  // 生成确认标记
  const generateConfirmMarker = (): string => {
    const userName = getCurrentUser();
    const timestamp = new Date().toISOString();
    return `-- CONFIRMED: ${userName} ${timestamp}`;
  };

  // 获取风险等级对应的颜色
  const getRiskColor = (level: string) => {
    switch (level) {
      case 'high':
        return 'red';
      case 'medium':
        return 'orange';
      case 'low':
        return 'green';
      default:
        return 'default';
    }
  };

  // 获取风险等级对应的图标
  const getRiskIcon = (level: string) => {
    switch (level) {
      case 'high':
        return <ExclamationCircleOutlined style={{ color: '#ff4d4f', fontSize: '24px' }} />;
      case 'medium':
        return <ExclamationCircleOutlined style={{ color: '#fa8c16', fontSize: '24px' }} />;
      case 'low':
        return <CheckCircleOutlined style={{ color: '#52c41a', fontSize: '24px' }} />;
      default:
        return null;
    }
  };

  // 获取操作类型的描述
  const getOperationDescription = (type: string): string => {
    const descriptions: Record<string, string> = {
      SELECT: '查询数据（只读）',
      INSERT: '插入数据（写操作）',
      UPDATE: '更新数据（写操作）',
      DELETE: '删除数据（高危）',
      DDL: '结构变更（高危）',
    };
    return descriptions[type] || '未知操作';
  };

  // 处理确认按钮点击
  const handleConfirm = async () => {
    setConfirmLoading(true);
    try {
      // 添加确认标记
      const confirmedSql = `${sql.trim()}\n\n${generateConfirmMarker()}`;
      
      // 调用父组件的确认回调
      await onConfirm(confirmedSql);
      
      message.success('操作成功');
    } catch (error: any) {
      message.error(`操作失败：${error.message}`);
    } finally {
      setConfirmLoading(false);
    }
  };

  // 格式化 SQL（简单的高亮）
  const formatSQL = (sqlText: string): string => {
    // 关键字高亮（简单实现）
    const keywords = ['SELECT', 'FROM', 'WHERE', 'INSERT', 'UPDATE', 'DELETE', 'INTO', 'SET', 'VALUES'];
    let formatted = sqlText;
    keywords.forEach(keyword => {
      const regex = new RegExp(`\\b${keyword}\\b`, 'gi');
      formatted = formatted.replace(regex, `<strong>${keyword}</strong>`);
    });
    return formatted;
  };

  return (
    <Modal
      title={
        <Space>
          {getRiskIcon(riskLevel)}
          <span>SQL 执行确认</span>
        </Space>
      }
      visible={visible}
      onCancel={onCancel}
      footer={[
        <Button key="cancel" onClick={onCancel} disabled={confirmLoading}>
          取消
        </Button>,
        <Button
          key="confirm"
          type="primary"
          danger={riskLevel === 'high'}
          loading={confirmLoading}
          onClick={handleConfirm}
        >
          {riskLevel === 'high' ? '确认执行（高危）' : '确认执行'}
        </Button>,
      ]}
      width={700}
      destroyOnClose
    >
      <Space direction="vertical" style={{ width: '100%' }} size="middle">
        {/* 操作信息 */}
        <Alert
          message={
            <Space>
              <DatabaseOutlined />
              <span>操作类型：{getOperationDescription(operationType)}</span>
            </Space>
          }
          description={
            <Space direction="vertical" style={{ width: '100%' }}>
              {description && <Paragraph style={{ marginBottom: 0 }}>{description}</Paragraph>}
              <Space size="large">
                <span>
                  风险等级：<Tag color={getRiskColor(riskLevel)}>{riskLevel.toUpperCase()}</Tag>
                </span>
                {tableName && (
                  <span>
                    表名：<Text code>{tableName}</Text>
                  </span>
                )}
                {affectedRows !== undefined && (
                  <span>
                    预计影响：<Text strong>{affectedRows}</Text> 行
                  </span>
                )}
              </Space>
            </Space>
          }
          type={riskLevel === 'high' ? 'error' : riskLevel === 'medium' ? 'warning' : 'info'}
          showIcon
        />

        {/* SQL 语句显示 */}
        <div>
          <Title level={5}>SQL 语句：</Title>
          <div
            style={{
              backgroundColor: '#f5f5f5',
              padding: '12px',
              borderRadius: '4px',
              fontFamily: 'monospace',
              fontSize: '13px',
              maxHeight: '300px',
              overflow: 'auto',
              border: '1px solid #d9d9d9',
            }}
          >
            <code dangerouslySetInnerHTML={{ __html: formatSQL(sql) }} />
          </div>
        </div>

        {/* 风险提示 */}
        {riskLevel === 'high' && (
          <Alert
            message="高危操作警告"
            description={
              <ul style={{ marginBottom: 0, paddingLeft: '20px' }}>
                <li>此操作可能导致数据丢失且不可恢复</li>
                <li>请确保已备份重要数据</li>
                <li>请仔细检查 SQL 语句和 WHERE 条件</li>
                <li>建议在测试环境先验证</li>
              </ul>
            }
            type="error"
            showIcon
          />
        )}

        {riskLevel === 'medium' && (
          <Alert
            message="操作提醒"
            description="此操作会修改数据，请确保了解操作的影响范围"
            type="warning"
            showIcon
          />
        )}
      </Space>
    </Modal>
  );
};

export default SQLConfirmModal;
