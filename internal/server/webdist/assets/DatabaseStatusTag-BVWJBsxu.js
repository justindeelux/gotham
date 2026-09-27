import{G as ye,bB as me,bC as xe,I as ne,an as o,b8 as se,H as J,V as y,aX as re,d as le,J as ce,bD as Q,o as x,c as E,a as F,M as p,T as V,S as b,l as te,Z as oe,N as ke,aI as Se,q as w,a5 as _e,y as B,at as Z,b3 as Ce,L as Be,b4 as $e,P as T,Q as ee,b1 as m,aw as ze,az as k,D as De,u as Re,w as Ve,x as Te,k as Fe,t as Pe}from"./index-BQvpBbOo.js";import{u as Ee}from"./use-merged-state-Cx0FWrJE.js";import{i as Ie,T as Ae}from"./servers-B5Qdslu2.js";import{_ as Ke}from"./_plugin-vue_export-helper-DlAUqK2U.js";function Me(e){const{primaryColor:r,opacityDisabled:l,borderRadius:s,textColor3:h}=e;return{...me,iconColor:h,textColor:"white",loadingColor:r,opacityDisabled:l,railColor:"rgba(0, 0, 0, .14)",railColorActive:r,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:s,railBorderRadiusMedium:s,railBorderRadiusLarge:s,buttonBorderRadiusSmall:s,buttonBorderRadiusMedium:s,buttonBorderRadiusLarge:s,boxShadowFocus:`0 0 0 2px ${xe(r,{alpha:.2})}`}}const Ne={common:ye,self:Me};var We=ne("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[o("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),o("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),o("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),ne("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[se({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),o("checked, unchecked",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),o("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),o("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),J("&:focus",[o("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),y("round",[o("rail","border-radius: calc(var(--n-rail-height) / 2);",[o("button","border-radius: calc(var(--n-button-height) / 2);")])]),re("disabled",[re("icon",[y("rubber-band",[y("pressed",[o("rail",[o("button","max-width: var(--n-button-width-pressed);")])]),o("rail",[J("&:active",[o("button","max-width: var(--n-button-width-pressed);")])]),y("active",[y("pressed",[o("rail",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),o("rail",[J("&:active",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),y("active",[o("rail",[o("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),o("rail",`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[o("button-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[se()]),o("button",`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),y("active",[o("rail","background-color: var(--n-rail-color-active);")]),y("loading",[o("rail",`
 cursor: wait;
 `)]),y("disabled",[o("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]);const Le=["aria-checked","tabindex","onClick","onFocus","onBlur","onKeyup","onKeydown"],Ue={...ce.props,size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]};let A;var sa=le({name:"Switch",props:Ue,slots:Object,setup(e){A===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?A=CSS.supports("width","max(1px)"):A=!1:A=!0);const{mergedClsPrefixRef:r,inlineThemeDisabled:l,mergedComponentPropsRef:s}=ke(e),h=ce("Switch","-switch",We,Ne,e,r),c=Se(e,{mergedSize(n){if(e.size!==void 0)return e.size;if(n)return n.mergedSize.value;const R=s?.value?.Switch?.size;return R||"medium"}}),{mergedSizeRef:v,mergedDisabledRef:d}=c,u=w(e.defaultValue),S=ze(e,"value"),g=Ee(S,u),$=B(()=>g.value===e.checkedValue),i=w(!1),f=w(!1),z=B(()=>{const{railStyle:n}=e;if(n)return n({focused:f.value,checked:$.value})});function P(n){const{"onUpdate:value":R,onChange:K,onUpdateValue:M}=e,{nTriggerFormInput:j,nTriggerFormChange:X}=c;R&&Z(R,n),M&&Z(M,n),K&&Z(K,n),u.value=n,j(),X()}function N(){const{nTriggerFormFocus:n}=c;n()}function W(){const{nTriggerFormBlur:n}=c;n()}function L(){e.loading||d.value||(g.value!==e.checkedValue?P(e.checkedValue):P(e.uncheckedValue))}function U(){f.value=!0,N()}function H(){f.value=!1,W(),i.value=!1}function O(n){e.loading||d.value||n.key===" "&&(g.value!==e.checkedValue?P(e.checkedValue):P(e.uncheckedValue),i.value=!1)}function t(n){e.loading||d.value||n.key===" "&&(n.preventDefault(),i.value=!0)}const a=B(()=>{const{value:n}=v,{self:{opacityDisabled:R,railColor:K,railColorActive:M,buttonBoxShadow:j,buttonColor:X,boxShadowFocus:de,loadingColor:ue,textColor:he,iconColor:fe,[T("buttonHeight",n)]:_,[T("buttonWidth",n)]:be,[T("buttonWidthPressed",n)]:ve,[T("railHeight",n)]:C,[T("railWidth",n)]:I,[T("railBorderRadius",n)]:ge,[T("buttonBorderRadius",n)]:pe},common:{cubicBezierEaseInOut:we}}=h.value;let q,Y,G;return A?(q=`calc((${C} - ${_}) / 2)`,Y=`max(${C}, ${_})`,G=`max(${I}, calc(${I} + ${_} - ${C}))`):(q=ee((m(C)-m(_))/2),Y=ee(Math.max(m(C),m(_))),G=m(C)>m(_)?I:ee(m(I)+m(_)-m(C))),{"--n-bezier":we,"--n-button-border-radius":pe,"--n-button-box-shadow":j,"--n-button-color":X,"--n-button-width":be,"--n-button-width-pressed":ve,"--n-button-height":_,"--n-height":Y,"--n-offset":q,"--n-opacity-disabled":R,"--n-rail-border-radius":ge,"--n-rail-color":K,"--n-rail-color-active":M,"--n-rail-height":C,"--n-rail-width":I,"--n-width":G,"--n-box-shadow-focus":de,"--n-loading-color":ue,"--n-text-color":he,"--n-icon-color":fe}}),D=l?_e("switch",B(()=>v.value[0]),a,e):void 0;return{handleClick:L,handleBlur:H,handleFocus:U,handleKeyup:O,handleKeydown:t,mergedRailStyle:z,pressed:i,mergedClsPrefix:r,mergedValue:g,checked:$,mergedDisabled:d,cssVars:l?void 0:a,themeClass:D?.themeClass,onRender:D?.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:r,checked:l,mergedRailStyle:s,onRender:h,$slots:c}=this;h?.();const{checked:v,unchecked:d,icon:u,"checked-icon":S,"unchecked-icon":g}=c,$=!(Q(u)&&Q(S)&&Q(g));return x(),E("div",{role:"switch","aria-checked":l,class:b([`${e}-switch`,this.themeClass,$&&`${e}-switch--icon`,l&&`${e}-switch--active`,r&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`]),tabindex:this.mergedDisabled?void 0:0,style:oe(this.cssVars),onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},[F("div",{class:b(`${e}-switch__rail`),"aria-hidden":"true",style:oe(s)},[p(()=>V(v,i=>V(d,f=>i||f?(x(),E("div",{key:4,"aria-hidden":!0,class:b(`${e}-switch__children-placeholder`)},[F("div",{class:b(`${e}-switch__rail-placeholder`)},[F("div",{class:b(`${e}-switch__button-placeholder`)},null,2),p(()=>i)],2),F("div",{class:b(`${e}-switch__rail-placeholder`)},[F("div",{class:b(`${e}-switch__button-placeholder`)},null,2),p(()=>f)],2)],2)):null))),F("div",{class:b(`${e}-switch__button`)},[p(()=>V(u,i=>V(S,f=>V(g,z=>(x(),te($e,null,{default:()=>this.loading?(x(),te(Ce,Be({key:"loading",clsPrefix:e,strokeWidth:20},this.spinProps),null,16,["clsPrefix"])):this.checked&&(f||i)?(x(),E("div",{class:b(`${e}-switch__button-icon`),key:f?"checked-icon":"icon"},[p(()=>f||i)],2)):!this.checked&&(z||i)?(x(),E("div",{class:b(`${e}-switch__button-icon`),key:z?"unchecked-icon":"icon"},[p(()=>z||i)],2)):null},1024)))))),p(()=>V(v,i=>i&&(x(),E("div",{key:"checked",class:b(`${e}-switch__checked`)},[p(()=>i)],2)))),p(()=>V(d,i=>i&&(x(),E("div",{key:"unchecked",class:b(`${e}-switch__unchecked`)},[p(()=>i)],2))))],2)],6)],46,Le)}});async function ie(){return(await k.get("/databases")).data.databases??[]}async function He(e){return(await k.get(`/databases/${e}`)).data.database}async function Oe(e){const r={name:e.name,engine:e.engine,server_id:e.server_id};e.version&&e.version.trim()!==""&&(r.version=e.version.trim()),e.public_port&&e.public_port>0&&(r.public_port=e.public_port);const l=await k.post("/databases",r);return{database:l.data.database,credentials:l.data.credentials}}async function je(e,r){return(await k.patch(`/databases/${e}`,{name:r.name})).data.database}async function Xe(e){await k.delete(`/databases/${e}`)}async function qe(e){return(await k.get(`/databases/${e}/credentials`)).data.credentials}async function Ye(e){return(await k.post(`/databases/${e}/start`,{})).data.database}async function Ge(e){return(await k.post(`/databases/${e}/stop`,{})).data.database}async function Je(e){return(await k.post(`/databases/${e}/restart`,{})).data.database}function ae(e){return Ie(e)?e.status===404?"Database not found. It may have been deleted or belong to another account.":e.status===409?"A database with that name already exists.":e.status===502?"The node agent is unreachable or the healthcheck failed. Check the node status and retry.":e.status===503?"Databases are disabled on the control plane (FEATURE_DATABASES=false).":e.message||"Request failed":e instanceof Error?e.message:"Something went wrong. Please try again."}const Qe=5e3,ra=De("databases",()=>{const e=w([]),r=w(!1),l=w(null),s=w(!1),h=w({}),c=w(!1),v=w(null);let d=null;function u(t){const a=e.value.findIndex(D=>D.id===t.id);if(a===-1){e.value=[t,...e.value];return}e.value[a]=t}async function S(){r.value=!0,l.value=null;try{e.value=await ie()}catch(t){throw l.value=ae(t),t}finally{r.value=!1}}async function g(){try{e.value=await ie(),l.value=null}catch(t){l.value=ae(t)}}function $(){d===null&&(d=setInterval(()=>{g()},Qe))}function i(){d!==null&&(clearInterval(d),d=null)}async function f(t){const a=await He(t);return u(a),a}async function z(t){s.value=!0;try{const a=await Oe(t);return u(a.database),h.value[a.database.id]=a.credentials,a}finally{s.value=!1}}async function P(t,a){s.value=!0;try{const D=await je(t,{name:a});return u(D),D}finally{s.value=!1}}async function N(t){s.value=!0;try{await Xe(t),e.value=e.value.filter(a=>a.id!==t),delete h.value[t]}finally{s.value=!1}}async function W(t){c.value=!0,v.value=null;try{const a=await qe(t);return h.value[t]=a,a}catch(a){throw v.value=ae(a),a}finally{c.value=!1}}function L(t){return h.value[t]??null}async function U(t){s.value=!0;try{const a=await Ye(t);return u(a),a}finally{s.value=!1}}async function H(t){s.value=!0;try{const a=await Ge(t);return u(a),a}finally{s.value=!1}}async function O(t){s.value=!0;try{const a=await Je(t);return u(a),a}finally{s.value=!1}}return{databases:e,loading:r,error:l,acting:s,credentialsById:h,credentialsLoading:c,credentialsError:v,fetchDatabases:S,refreshDatabases:g,pollDatabases:$,stopPolling:i,fetchDatabase:f,provision:z,rename:P,remove:N,fetchCredentials:W,credentialsOf:L,start:U,stop:H,restart:O}}),Ze=le({__name:"DatabaseStatusTag",props:{status:{},size:{default:"small"}},setup(e){const r={creating:"info",running:"success",stopped:"warning",error:"error",deleting:"default"},l={creating:"Creating",running:"Running",stopped:"Stopped",error:"Error",deleting:"Deleting"},s={creating:"dot--creating",running:"dot--running",stopped:"dot--stopped",error:"dot--error",deleting:"dot--deleting"},h=new Set(["creating"]),c=e,v=B(()=>r[c.status]??"default"),d=B(()=>l[c.status]??c.status),u=B(()=>s[c.status]??"dot--deleting"),S=B(()=>h.has(c.status));return(g,$)=>(x(),te(Re(Ae),{type:v.value,size:e.size,round:""},{default:Ve(()=>[F("span",{class:Te(["status-dot",[u.value,{"dot-pulse":S.value}]]),"aria-hidden":"true"},null,2),Fe(" "+Pe(d.value),1)]),_:1},8,["type","size"]))}}),oa=Ke(Ze,[["__scopeId","data-v-7260dfc2"]]);export{oa as D,sa as S,ae as d,ra as u};
