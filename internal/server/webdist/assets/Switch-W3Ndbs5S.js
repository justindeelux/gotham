import{b6 as ve,gQ as ge,gR as me,b8 as Y,bZ as o,f1 as X,b7 as N,bY as s,f2 as Q,d as we,b9 as G,gS as U,o as b,c as x,a as k,bb as l,bI as i,_ as Z,bc as pe,gs as ye,j as H,c2 as xe,f as D,c4 as O,f6 as p,p as q,fD as ke,ba as Se,f7 as Ce,be as y,bf as E,fA as c,c7 as Be}from"./index-DswNNeRv.js";import{u as _e}from"./use-merged-state-1JdeCq7A.js";function Re(e){const{primaryColor:d,opacityDisabled:f,borderRadius:n,textColor3:S}=e;return{...ge,iconColor:S,textColor:"white",loadingColor:d,opacityDisabled:f,railColor:"rgba(0, 0, 0, .14)",railColorActive:d,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:n,railBorderRadiusMedium:n,railBorderRadiusLarge:n,buttonBorderRadiusSmall:n,buttonBorderRadiusMedium:n,buttonBorderRadiusLarge:n,boxShadowFocus:`0 0 0 2px ${me(d,{alpha:.2})}`}}const $e={common:ve,self:Re};var ze=Y("switch",`
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
 `),Y("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[X({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),o("checked, unchecked",`
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
 `),N("&:focus",[o("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),s("round",[o("rail","border-radius: calc(var(--n-rail-height) / 2);",[o("button","border-radius: calc(var(--n-button-height) / 2);")])]),Q("disabled",[Q("icon",[s("rubber-band",[s("pressed",[o("rail",[o("button","max-width: var(--n-button-width-pressed);")])]),o("rail",[N("&:active",[o("button","max-width: var(--n-button-width-pressed);")])]),s("active",[s("pressed",[o("rail",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),o("rail",[N("&:active",[o("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),s("active",[o("rail",[o("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),o("rail",`
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
 `,[X()]),o("button",`
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
 `)]),s("active",[o("rail","background-color: var(--n-rail-color-active);")]),s("loading",[o("rail",`
 cursor: wait;
 `)]),s("disabled",[o("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]);const Ve=["aria-checked","tabindex","onClick","onFocus","onBlur","onKeyup","onKeydown"],Fe={...G.props,size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]};let $;var De=we({name:"Switch",props:Fe,slots:Object,setup(e){$===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?$=CSS.supports("width","max(1px)"):$=!1:$=!0);const{mergedClsPrefixRef:d,inlineThemeDisabled:f,mergedComponentPropsRef:n}=pe(e),S=G("Switch","-switch",ze,$e,e,d),v=ye(e,{mergedSize(t){if(e.size!==void 0)return e.size;if(t)return t.mergedSize.value;const w=n?.value?.Switch?.size;return w||"medium"}}),{mergedSizeRef:C,mergedDisabledRef:g}=v,B=H(e.defaultValue),z=Be(e,"value"),m=_e(z,B),V=D(()=>m.value===e.checkedValue),a=H(!1),r=H(!1),_=D(()=>{const{railStyle:t}=e;if(t)return t({focused:r.value,checked:V.value})});function F(t){const{"onUpdate:value":w,onChange:P,onUpdateValue:T}=e,{nTriggerFormInput:K,nTriggerFormChange:W}=v;w&&O(w,t),T&&O(T,t),P&&O(P,t),B.value=t,K(),W()}function J(){const{nTriggerFormFocus:t}=v;t()}function ee(){const{nTriggerFormBlur:t}=v;t()}function te(){e.loading||g.value||(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue))}function oe(){r.value=!0,J()}function ae(){r.value=!1,ee(),a.value=!1}function ie(t){e.loading||g.value||t.key===" "&&(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue),a.value=!1)}function ne(t){e.loading||g.value||t.key===" "&&(t.preventDefault(),a.value=!0)}const I=D(()=>{const{value:t}=C,{self:{opacityDisabled:w,railColor:P,railColorActive:T,buttonBoxShadow:K,buttonColor:W,boxShadowFocus:re,loadingColor:le,textColor:se,iconColor:ce,[y("buttonHeight",t)]:u,[y("buttonWidth",t)]:de,[y("buttonWidthPressed",t)]:ue,[y("railHeight",t)]:h,[y("railWidth",t)]:R,[y("railBorderRadius",t)]:he,[y("buttonBorderRadius",t)]:be},common:{cubicBezierEaseInOut:fe}}=S.value;let M,j,A;return $?(M=`calc((${h} - ${u}) / 2)`,j=`max(${h}, ${u})`,A=`max(${R}, calc(${R} + ${u} - ${h}))`):(M=E((c(h)-c(u))/2),j=E(Math.max(c(h),c(u))),A=c(h)>c(u)?R:E(c(R)+c(u)-c(h))),{"--n-bezier":fe,"--n-button-border-radius":be,"--n-button-box-shadow":K,"--n-button-color":W,"--n-button-width":de,"--n-button-width-pressed":ue,"--n-button-height":u,"--n-height":j,"--n-offset":M,"--n-opacity-disabled":w,"--n-rail-border-radius":he,"--n-rail-color":P,"--n-rail-color-active":T,"--n-rail-height":h,"--n-rail-width":R,"--n-width":A,"--n-box-shadow-focus":re,"--n-loading-color":le,"--n-text-color":se,"--n-icon-color":ce}}),L=f?xe("switch",D(()=>C.value[0]),I,e):void 0;return{handleClick:te,handleBlur:ae,handleFocus:oe,handleKeyup:ie,handleKeydown:ne,mergedRailStyle:_,pressed:a,mergedClsPrefix:d,mergedValue:m,checked:V,mergedDisabled:g,cssVars:f?void 0:I,themeClass:L?.themeClass,onRender:L?.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:d,checked:f,mergedRailStyle:n,onRender:S,$slots:v}=this;S?.();const{checked:C,unchecked:g,icon:B,"checked-icon":z,"unchecked-icon":m}=v,V=!(U(B)&&U(z)&&U(m));return b(),x("div",{role:"switch","aria-checked":f,class:i([`${e}-switch`,this.themeClass,V&&`${e}-switch--icon`,f&&`${e}-switch--active`,d&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`]),tabindex:this.mergedDisabled?void 0:0,style:Z(this.cssVars),onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},[k("div",{class:i(`${e}-switch__rail`),"aria-hidden":"true",style:Z(n)},[l(()=>p(C,a=>p(g,r=>a||r?(b(),x("div",{key:4,"aria-hidden":!0,class:i(`${e}-switch__children-placeholder`)},[k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>a)],2),k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>r)],2)],2)):null))),k("div",{class:i(`${e}-switch__button`)},[l(()=>p(B,a=>p(z,r=>p(m,_=>(b(),q(Ce,null,{default:()=>this.loading?(b(),q(ke,Se({key:"loading",clsPrefix:e,strokeWidth:20},this.spinProps),null,16,["clsPrefix"])):this.checked&&(r||a)?(b(),x("div",{class:i(`${e}-switch__button-icon`),key:r?"checked-icon":"icon"},[l(()=>r||a)],2)):!this.checked&&(_||a)?(b(),x("div",{class:i(`${e}-switch__button-icon`),key:_?"unchecked-icon":"icon"},[l(()=>_||a)],2)):null},1024)))))),l(()=>p(C,a=>a&&(b(),x("div",{key:"checked",class:i(`${e}-switch__checked`)},[l(()=>a)],2)))),l(()=>p(g,a=>a&&(b(),x("div",{key:"unchecked",class:i(`${e}-switch__unchecked`)},[l(()=>a)],2))))],2)],6)],46,Ve)}});export{De as S};
