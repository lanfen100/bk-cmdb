/*
 * Tencent is pleased to support the open source community by making 蓝鲸 available.
 * Copyright (C) 2017-2018 THL A29 Limited, a Tencent company. All rights reserved.
 * Licensed under the MIT License (the "License"); you may not use this file except
 * in compliance with the License. You may obtain a copy of the License at
 * http://opensource.org/licenses/MIT
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions and
 * limitations under the License.
 */

package service

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/emicklei/go-restful"

	"configcenter/src/common"
	"configcenter/src/common/blog"
	"configcenter/src/common/core/cc/api"
	"configcenter/src/common/errors"
	"configcenter/src/common/http/httpserver"
	"configcenter/src/common/language"
	frtypes "configcenter/src/common/mapstr"
	"configcenter/src/common/util"
	"configcenter/src/scene_server/topo_server/app/options"
	"configcenter/src/scene_server/topo_server/core"
	"configcenter/src/scene_server/topo_server/core/types"
)

// TopoServiceInterface the topo service methods used to init
type TopoServiceInterface interface {
	SetOperation(operation core.Core, err errors.CCErrorIf, language language.CCLanguageIf)
	WebService() *restful.WebService
	SetConfig(cfg options.Config)
}

// New ceate topo servcie instance
func New() TopoServiceInterface {
	return &topoService{}
}

// topoService topo service
type topoService struct {
	language language.CCLanguageIf
	err      errors.CCErrorIf
	actions  []action
	core     core.Core
	cfg      options.Config
}

func (s *topoService) SetConfig(cfg options.Config) {
	s.cfg = cfg
}

// SetOperation set the operation
func (s *topoService) SetOperation(operation core.Core, err errors.CCErrorIf, language language.CCLanguageIf) {

	s.core = operation
	s.err = err
	s.language = language

}

// WebService the web service
func (s *topoService) WebService() *restful.WebService {

	// init service actions
	s.initService()

	ws := new(restful.WebService)
	/*
		    now ignore
			getErrFun := func() errors.CCErrorIf {
				return s.err
			}
			//ws.Path("/topo/v3").Filter(rdapi.AllGlobalFilter(getErrFun)).Produces(restful.MIME_JSON).Consumes(restful.MIME_JSON)
	*/
	ws.Path("/topo/{version}").Produces(restful.MIME_JSON).Consumes(restful.MIME_JSON) // TODO: {version} need to replaced by v3

	innerActions := s.Actions()

	for _, actionItem := range innerActions {
		switch actionItem.Verb {
		case http.MethodPost:
			ws.Route(ws.POST(actionItem.Path).To(actionItem.Handler))
		case http.MethodDelete:
			ws.Route(ws.DELETE(actionItem.Path).To(actionItem.Handler))
		case http.MethodPut:
			ws.Route(ws.PUT(actionItem.Path).To(actionItem.Handler))
		case http.MethodGet:
			ws.Route(ws.GET(actionItem.Path).To(actionItem.Handler))
		default:
			blog.Errorf(" the url (%s), the http method (%s) is not supported", actionItem.Path, actionItem.Verb)
		}
	}

	return ws
}

func (s *topoService) createAPIRspStr(errcode int, info interface{}) (string, error) {
	rsp := api.BKAPIRsp{
		Result:  true,
		Code:    0,
		Message: nil,
		Data:    nil,
	}

	if common.CCSuccess != errcode {
		rsp.Result = false
		rsp.Code = errcode
		rsp.Message = info
	} else {
		rsp.Message = common.CCSuccessStr
		rsp.Data = info
	}

	data, err := json.Marshal(rsp)
	return string(data), err
}

func (s *topoService) sendResponse(resp *restful.Response, errorCode int, dataMsg interface{}) {
	resp.Header().Set("Content-Type", "application/json")
	if rsp, rspErr := s.createAPIRspStr(errorCode, dataMsg); nil == rspErr {
		io.WriteString(resp, rsp)
	} else {
		blog.Errorf("failed to send response , error info is %s", rspErr.Error())
	}
}

// Actions return the all actions
func (s *topoService) Actions() []*httpserver.Action {

	var httpactions []*httpserver.Action
	for _, a := range s.actions {

		func(act action) {

			httpactions = append(httpactions, &httpserver.Action{Verb: act.Method, Path: act.Path, Handler: func(req *restful.Request, resp *restful.Response) {

				ownerID := util.GetActionOnwerID(req)
				//user := util.GetActionUser(req)

				// get the language
				language := util.GetActionLanguage(req)

				defLang := s.language.CreateDefaultCCLanguageIf(language)

				// get the error info by the language
				defErr := s.err.CreateDefaultCCErrorIf(language)

				value, err := ioutil.ReadAll(req.Request.Body)
				if err != nil {
					blog.Errorf("read http request body failed, error:%s", err.Error())
					errStr := defErr.Error(common.CCErrCommHTTPReadBodyFailed)
					s.sendResponse(resp, common.CCErrCommHTTPReadBodyFailed, errStr)
					return
				}

				mData := frtypes.MapStr{}
				if err := json.Unmarshal(value, &mData); nil != err && 0 != len(value) {
					blog.Errorf("failed to unmarshal the data, error %s", err.Error())
					errStr := defErr.Error(common.CCErrCommJSONUnmarshalFailed)
					s.sendResponse(resp, common.CCErrCommJSONUnmarshalFailed, errStr)
					return
				}

				data, dataErr := act.HandlerFunc(types.ContextParams{
					Err:             defErr,
					Lang:            defLang,
					Header:          req.Request.Header,
					SupplierAccount: ownerID,
				},
					req.PathParameter,
					req.QueryParameter,
					mData)

				if nil != dataErr {
					blog.Errorf("%s", dataErr.Error())
					switch e := dataErr.(type) {
					default:
						s.sendResponse(resp, common.CCSystemBusy, dataErr.Error())
					case errors.CCErrorCoder:
						s.sendResponse(resp, e.GetCode(), dataErr.Error())
					}
					return
				}

				s.sendResponse(resp, common.CCSuccess, data)

			}})
		}(a)

	}
	return httpactions
}
