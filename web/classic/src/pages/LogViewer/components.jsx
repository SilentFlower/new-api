/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import {
  Button,
  Input,
  Card,
  Form,
  Empty,
  Descriptions,
  Skeleton,
  Avatar,
  Tabs,
  TabPane,
  Typography,
  Spin,
  DatePicker,
  RadioGroup,
  Radio,
} from '@douyinfe/semi-ui';
import {
  IconSearch,
  IconKey,
  IconCoinMoneyStroked,
  IconTextStroked,
  IconStopwatchStroked,
  IconSend,
  IconRefresh,
} from '@douyinfe/semi-icons';
import {
  PieChart,
  Activity,
  KeyRound,
  CalendarClock,
} from 'lucide-react';
import { VChart } from '@visactor/react-vchart';

import {
  renderQuota,
  renderNumber,
} from '../../helpers';
import {
  CHART_CONFIG,
  CARD_PROPS,
} from '../../constants/dashboard.constants';
import CardTable from '../../components/common/ui/CardTable';
import {
  IllustrationNoResult,
  IllustrationNoResultDark,
} from '@douyinfe/semi-illustrations';

export function TokenLogAuthPanel(props) {
  return (
    <div className='px-4 py-4'>
      <div className='max-w-lg mx-auto mt-20'>
        <Card
          {...CARD_PROPS}
          className='!rounded-2xl'
          title={
            <div className='flex items-center gap-2'>
              <KeyRound size={18} />
              {props.t('API Key 日志查看器')}
            </div>
          }
        >
          <div className='space-y-4'>
            <Typography.Text type='secondary'>
              {props.t('输入您的 API Key 以查看使用日志和统计数据')}
            </Typography.Text>
            <Input
              prefix={<IconKey />}
              placeholder={props.t('请输入 API Key（sk-...）')}
              value={props.apiKey}
              onChange={props.setApiKey}
              onEnterPress={props.onAuth}
              size='large'
              showClear
            />
            {props.authError && (
              <Typography.Text type='danger'>{props.authError}</Typography.Text>
            )}
            <Button
              theme='solid'
              type='primary'
              block
              loading={props.authLoading}
              onClick={props.onAuth}
              size='large'
            >
              {props.t('查询')}
            </Button>
          </div>
        </Card>
      </div>
    </div>
  );
}

export function TokenLogHeader(props) {
  return (
    <div className='flex flex-col sm:flex-row justify-between items-start sm:items-center gap-2'>
      <div className='flex items-center gap-2'>
        <KeyRound size={18} />
        <Typography.Title heading={5} style={{ margin: 0 }}>
          {props.t('API Key 日志查看器')}
        </Typography.Title>
      </div>
      <Button type='tertiary' size='small' onClick={props.onSwitchKey}>
        {props.t('切换 Key')}
      </Button>
    </div>
  );
}

export function TokenLogTimeRangeControls(props) {
  return (
    <div className='flex flex-col sm:flex-row items-start sm:items-center gap-3 p-3 bg-[var(--semi-color-fill-0)] rounded-xl'>
      <div className='flex items-center gap-2 text-sm text-[var(--semi-color-text-2)]'>
        <CalendarClock size={16} />
        <span>{props.t('数据范围')}</span>
      </div>
      <RadioGroup
        type='button'
        buttonSize='small'
        value={props.timePreset}
        onChange={(e) => {
          props.setTimePreset(e.target.value);
          if (e.target.value !== 'custom') {
            props.setCustomDateRange(null);
          }
        }}
      >
        <Radio value='today'>{props.t('今天')}</Radio>
        <Radio value='7d'>{props.t('7 天')}</Radio>
        <Radio value='30d'>{props.t('30 天')}</Radio>
        <Radio value='custom'>{props.t('自定义')}</Radio>
      </RadioGroup>
      {props.timePreset === 'custom' && (
        <DatePicker
          type='dateTimeRange'
          value={props.customDateRange}
          onChange={(value) => props.setCustomDateRange(value)}
          placeholder={[props.t('开始时间'), props.t('结束时间')]}
          size='small'
          density='compact'
          style={{ width: 360 }}
        />
      )}
      <Button
        icon={<IconRefresh />}
        type='tertiary'
        size='small'
        onClick={props.onRefresh}
        loading={props.loading}
      />
    </div>
  );
}

export function TokenLogStatsCards(props) {
  if (!props.stat) return null;
  const cards = [
    {
      title: props.t('使用次数'),
      value: props.stat.count?.toLocaleString() || '0',
      icon: <IconSend />,
      avatarColor: 'green',
      bgColor: 'bg-green-50',
    },
    {
      title: props.t('消耗额度'),
      value: renderQuota(props.stat.quota || 0),
      icon: <IconCoinMoneyStroked />,
      avatarColor: 'yellow',
      bgColor: 'bg-yellow-50',
    },
    {
      title: props.t('Token 用量'),
      value: (
        props.stat.total_tokens ??
        (props.stat.prompt_tokens || 0) + (props.stat.completion_tokens || 0)
      ).toLocaleString(),
      icon: <IconTextStroked />,
      avatarColor: 'blue',
      bgColor: 'bg-blue-50',
    },
    {
      title: 'RPM / TPM',
      value: `${props.stat.rpm || 0} / ${props.stat.tpm || 0}`,
      icon: <IconStopwatchStroked />,
      avatarColor: 'purple',
      bgColor: 'bg-purple-50',
    },
  ];

  return (
    <div className='mb-4'>
      <div className='grid grid-cols-2 lg:grid-cols-4 gap-4'>
        {cards.map((card, idx) => (
          <Card
            key={idx}
            {...CARD_PROPS}
            className={`${card.bgColor} border-0 !rounded-2xl`}
          >
            <div className='flex items-center'>
              <Avatar className='mr-3' size='small' color={card.avatarColor}>
                {card.icon}
              </Avatar>
              <div>
                <div className='text-xs text-gray-500'>{card.title}</div>
                <div className='text-lg font-semibold'>
                  <Skeleton
                    loading={props.loading}
                    active
                    placeholder={
                      <Skeleton.Paragraph
                        active
                        rows={1}
                        style={{
                          width: '65px',
                          height: '24px',
                          marginTop: '4px',
                        }}
                      />
                    }
                  >
                    {card.value}
                  </Skeleton>
                </div>
              </div>
            </div>
          </Card>
        ))}
      </div>
    </div>
  );
}

export function TokenLogChartsPanel(props) {
  return (
    <div className='mb-4'>
      <Card
        {...CARD_PROPS}
        className='!rounded-2xl'
        title={
          <div className='flex flex-col lg:flex-row lg:items-center lg:justify-between w-full gap-3'>
            <div className='flex items-center gap-2'>
              <PieChart size={16} />
              {props.t('模型数据分析')}
            </div>
            <Tabs
              type='slash'
              activeKey={props.activeChartTab}
              onChange={props.setActiveChartTab}
            >
              <TabPane tab={<span>{props.t('调用次数分布')}</span>} itemKey='1' />
              <TabPane tab={<span>{props.t('消耗分布')}</span>} itemKey='2' />
            </Tabs>
          </div>
        }
        bodyStyle={{ padding: 0 }}
      >
        <Spin spinning={props.loading}>
          <div className='h-96 p-2'>
            {props.activeChartTab === '1' && (
              <VChart spec={props.specPie} option={CHART_CONFIG} />
            )}
            {props.activeChartTab === '2' && (
              <VChart spec={props.specLine} option={CHART_CONFIG} />
            )}
          </div>
        </Spin>
      </Card>
    </div>
  );
}

export function TokenLogTablePanel(props) {
  return (
    <div className='mb-4'>
      <Card
        {...CARD_PROPS}
        className='!rounded-2xl'
        title={
          <div className='flex items-center gap-2'>
            <Activity size={16} />
            {props.t('使用日志')}
          </div>
        }
      >
        <Form
          initValues={{
            model_name: '',
            request_id: '',
            logType: '0',
          }}
          getFormApi={props.setFormApi}
          onSubmit={props.onRefresh}
          allowEmpty={true}
          autoComplete='off'
          layout='vertical'
        >
          <div className='flex flex-col gap-2 mb-4'>
            <div className='grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2'>
              <Form.Input
                field='model_name'
                prefix={<IconSearch />}
                placeholder={props.t('模型名称')}
                showClear
                pure
                size='small'
              />
              <Form.Input
                field='request_id'
                prefix={<IconSearch />}
                placeholder={props.t('Request ID')}
                showClear
                pure
                size='small'
              />
            </div>
            <div className='flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3'>
              <div className='w-full sm:w-auto'>
                <Form.Select
                  field='logType'
                  placeholder={props.t('日志类型')}
                  className='w-full sm:w-auto min-w-[120px]'
                  showClear
                  pure
                  onChange={() => {
                    setTimeout(() => props.onRefresh(), 0);
                  }}
                  size='small'
                >
                  <Form.Select.Option value='0'>
                    {props.t('全部')}
                  </Form.Select.Option>
                  <Form.Select.Option value='2'>
                    {props.t('消费')}
                  </Form.Select.Option>
                  <Form.Select.Option value='5'>
                    {props.t('错误')}
                  </Form.Select.Option>
                  <Form.Select.Option value='6'>
                    {props.t('退款')}
                  </Form.Select.Option>
                </Form.Select>
              </div>
              <div className='flex gap-2 w-full sm:w-auto justify-end'>
                <Button
                  type='tertiary'
                  htmlType='submit'
                  loading={props.loading}
                  size='small'
                >
                  {props.t('查询')}
                </Button>
                <Button
                  type='tertiary'
                  onClick={() => {
                    if (props.formApi) {
                      props.formApi.reset();
                      setTimeout(() => props.onRefresh(), 100);
                    }
                  }}
                  size='small'
                >
                  {props.t('重置')}
                </Button>
              </div>
            </div>
          </div>
        </Form>

        <CardTable
          columns={props.tableColumns}
          {...(props.hasExpandableRows() && {
            expandedRowRender: (record) => (
              <Descriptions data={props.expandData[record.key]} />
            ),
            expandRowByClick: true,
            rowExpandable: (record) =>
              props.expandData[record.key] &&
              props.expandData[record.key].length > 0,
          })}
          dataSource={props.logs}
          rowKey='key'
          loading={props.loading}
          scroll={{ x: 'max-content' }}
          className='rounded-xl overflow-hidden'
          size='small'
          empty={
            <Empty
              image={<IllustrationNoResult style={{ width: 150, height: 150 }} />}
              darkModeImage={
                <IllustrationNoResultDark
                  style={{ width: 150, height: 150 }}
                />
              }
              description={props.t('搜索无结果')}
              style={{ padding: 30 }}
            />
          }
          pagination={{
            currentPage: props.activePage,
            pageSize: props.pageSize,
            total: props.logCount,
            pageSizeOptions: [10, 20, 50, 100],
            showSizeChanger: true,
            onPageSizeChange: props.onPageSizeChange,
            onPageChange: props.onPageChange,
          }}
        />
      </Card>
    </div>
  );
}
