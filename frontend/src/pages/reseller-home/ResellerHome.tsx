import { useEffect, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Descriptions,
  Form,
  Input,
  Layout,
  Modal,
  QRCode,
  Result,
  Row,
  Space,
  Spin,
  Tag,
  message,
} from 'antd';
import * as OTPAuth from 'otpauth';

import AppSidebar from '@/layouts/AppSidebar';
import PoolUsage from '@/components/resellers/PoolUsage';
import { useResellerMe, useResellerSelf, useResellerSubSettings } from '@/api/queries/useResellers';
import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { usePageTitle } from '@/hooks/usePageTitle';
import { IntlUtil, RandomUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';

export default function ResellerHome() {
  usePageTitle();
  const { t } = useTranslation();
  const { antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => setMessageInstance(messageApi), [messageApi]);
  const me = useResellerMe();
  const settings = useResellerSubSettings();
  const r = me.data?.reseller;

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className="reseller-home">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={me.isPending} delay={200} size="large">
              {me.isError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  extra={<Button onClick={() => me.refetch()}>{t('refresh')}</Button>}
                />
              ) : r ? (
                <Row gutter={[isMobile ? 8 : 16, isMobile ? 8 : 16]}>
                  <Col span={24}>
                    <Card title={r.name || r.username}>
                      <PoolUsage
                        poolBytes={r.poolBytes}
                        usedTraffic={r.usedTraffic}
                        state={r.state}
                      />
                    </Card>
                  </Col>
                  <Col xs={24} md={12}>
                    <Card>
                      <Descriptions column={1} size="small">
                        <Descriptions.Item label={t('pages.resellers.clients')}>
                          <span data-testid="client-count">
                            {r.maxClients > 0
                              ? `${r.clientCount} / ${r.maxClients}`
                              : r.clientCount}
                          </span>
                        </Descriptions.Item>
                        <Descriptions.Item label={t('pages.resellers.expiry')}>
                          {r.expiryTime > 0
                            ? IntlUtil.formatDate(
                                r.expiryTime,
                                settings.data?.datepicker ?? 'gregorian',
                              )
                            : t('pages.resellers.never')}
                        </Descriptions.Item>
                      </Descriptions>
                    </Card>
                  </Col>
                  <Col xs={24} md={12}>
                    <AccountCard twoFactorEnabled={r.twoFactorEnabled} username={r.username} />
                  </Col>
                </Row>
              ) : (
                <div className="loading-spacer" />
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}

function AccountCard({
  twoFactorEnabled,
  username,
}: {
  twoFactorEnabled: boolean;
  username: string;
}) {
  const { t } = useTranslation();
  const uid = useId();
  const { changePassword, enableTotp, disableTotp } = useResellerSelf();
  const [open, setOpen] = useState<'' | 'password' | 'totp-on' | 'totp-off'>('');
  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [code, setCode] = useState('');
  const secret = useMemo(
    () => (open === 'totp-on' ? RandomUtil.randomBase32String(20) : ''),
    [open],
  );
  const otpUri = useMemo(
    () =>
      secret
        ? new OTPAuth.TOTP({
            issuer: 'netron-panel',
            label: username,
            algorithm: 'SHA1',
            digits: 6,
            period: 30,
            secret,
          }).toString()
        : '',
    [secret, username],
  );
  const close = () => {
    setOpen('');
    setCurrent('');
    setNext('');
    setCode('');
  };
  const submit = async () => {
    const msg =
      open === 'password'
        ? await changePassword.mutateAsync({ currentPassword: current, newPassword: next })
        : open === 'totp-on'
          ? await enableTotp.mutateAsync({ secret, code })
          : await disableTotp.mutateAsync(code);
    if (msg?.success) close();
  };

  return (
    <Card title={t('pages.resellerHome.account')}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <Button onClick={() => setOpen('password')}>
          {t('pages.resellerHome.changePassword')}
        </Button>
        <Space wrap>
          <span>{t('pages.settings.security.twoFactor')}</span>
          <Tag color={twoFactorEnabled ? 'green' : 'default'}>
            {twoFactorEnabled
              ? t('pages.resellerHome.twoFactorOn')
              : t('pages.resellerHome.twoFactorOff')}
          </Tag>
          <Button size="small" onClick={() => setOpen(twoFactorEnabled ? 'totp-off' : 'totp-on')}>
            {twoFactorEnabled
              ? t('pages.resellerHome.disableTwoFactor')
              : t('pages.resellerHome.enableTwoFactor')}
          </Button>
        </Space>
      </Space>
      <Modal
        open={open !== ''}
        title={
          open === 'password'
            ? t('pages.resellerHome.changePassword')
            : t('pages.settings.security.twoFactor')
        }
        okText={t('confirm')}
        cancelText={t('cancel')}
        onCancel={close}
        onOk={submit}
        confirmLoading={changePassword.isPending || enableTotp.isPending || disableTotp.isPending}
        destroyOnHidden
      >
        <Form layout="vertical">
          {open === 'password' ? (
            <>
              <Form.Item label={t('pages.resellerHome.currentPassword')} htmlFor={`${uid}-f1`}>
                <Input.Password
                  id={`${uid}-f1`}
                  value={current}
                  onChange={(e) => setCurrent(e.target.value)}
                  autoComplete="current-password"
                />
              </Form.Item>
              <Form.Item
                label={t('pages.resellers.newPassword')}
                htmlFor={`${uid}-f2`}
                extra={t('pages.resellers.passwordRule')}
              >
                <Input.Password
                  id={`${uid}-f2`}
                  value={next}
                  onChange={(e) => setNext(e.target.value)}
                  autoComplete="new-password"
                />
              </Form.Item>
            </>
          ) : (
            <>
              {open === 'totp-on' && (
                <Form.Item extra={t('pages.resellerHome.twoFactorScan')}>
                  <Space direction="vertical" align="center" style={{ width: '100%' }}>
                    <QRCode value={otpUri} />
                    <code>{secret}</code>
                  </Space>
                </Form.Item>
              )}
              <Form.Item label={t('twoFactorCode')} htmlFor={`${uid}-f3`}>
                <Input
                  id={`${uid}-f3`}
                  value={code}
                  onChange={(e) => setCode(e.target.value.trim())}
                  inputMode="numeric"
                  maxLength={6}
                />
              </Form.Item>
            </>
          )}
        </Form>
      </Modal>
    </Card>
  );
}
